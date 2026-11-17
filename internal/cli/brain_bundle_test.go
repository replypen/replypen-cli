package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func mustGitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commitIn(t *testing.T, dir, file, body string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mustGitT(t, dir, "add", file)
	mustGitT(t, dir, "commit", "-q", "-m", "edit "+file)
	return mustGitT(t, dir, "rev-parse", "HEAD")
}

// fakeBundle is the server side of the bundle transport over real git repos: the project brain and one
// tenant overlay ("t1"). Download, upload one head into dev/<name>, fast-forward-only land. remoteMode
// "github" makes status say so (the CLI checks status first).
type fakeBundle struct {
	srv           *httptest.Server
	box, tenant   string
	uploads       map[string]int // per brain ("" = project, else tenant slug)
	contentLength []int64
	syncs         int
	beforeSync    func(box string) // runs before each land, e.g. to move main under the client
}

func fakeBundleServer(t *testing.T, remoteMode string) (*httptest.Server, string) {
	f := newFakeBundle(t, remoteMode)
	return f.srv, f.box
}

func newFakeBundle(t *testing.T, remoteMode string) *fakeBundle {
	t.Helper()
	f := &fakeBundle{box: t.TempDir(), tenant: t.TempDir(), uploads: map[string]int{}}
	for _, dir := range []string{f.box, f.tenant} {
		mustGitT(t, dir, "init", "-q", "-b", "main")
		commitIn(t, dir, "AGENTS.md", "brain\n")
	}
	boxOf := func(r *http.Request) string {
		if r.PathValue("slug") == "t1" {
			return f.tenant
		}
		return f.box
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/whoami", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"project":{"id":"p1","name":"alpha"}}`))
	})
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"id":"p1","name":"alpha"}]}`))
	})
	for _, base := range []string{"/api/v1/projects/alpha", "/api/v1/projects/alpha/tenants/{slug}"} {
		mux.HandleFunc("GET "+base+"/brain/status", func(w http.ResponseWriter, r *http.Request) {
			_, _ = fmt.Fprintf(w, `{"project":"alpha","status":{"available":true,"ref":"main","state":"local_only","remote_mode":%q,"local_sha":%q}}`,
				remoteMode, mustGitT(t, boxOf(r), "rev-parse", "main"))
		})
		mux.HandleFunc("GET "+base+"/brain/bundle", func(w http.ResponseWriter, r *http.Request) {
			path := filepath.Join(t.TempDir(), "dl.bundle")
			mustGitT(t, boxOf(r), "bundle", "create", "-q", path, "--branches", "--tags")
			raw, _ := os.ReadFile(path)
			_, _ = w.Write(raw)
		})
		mux.HandleFunc("POST "+base+"/brain/refs/{branch}", func(w http.ResponseWriter, r *http.Request) {
			f.uploads[r.PathValue("slug")]++
			f.contentLength = append(f.contentLength, r.ContentLength)
			path := filepath.Join(t.TempDir(), "up.bundle")
			raw, _ := io.ReadAll(r.Body)
			_ = os.WriteFile(path, raw, 0o600)
			ref := "dev/" + r.PathValue("branch")
			box := boxOf(r)
			mustGitT(t, box, "fetch", "-q", path, "+"+r.URL.Query().Get("head")+":refs/heads/"+ref)
			_, _ = fmt.Fprintf(w, `{"project":"alpha","ref":%q,"sha":%q}`, ref, mustGitT(t, box, "rev-parse", ref))
		})
		mux.HandleFunc("POST "+base+"/brain/sync", func(w http.ResponseWriter, r *http.Request) {
			var body struct{ Ref string }
			_ = json.NewDecoder(r.Body).Decode(&body)
			box := boxOf(r)
			f.syncs++
			if f.beforeSync != nil {
				f.beforeSync(box)
			}
			before := mustGitT(t, box, "rev-parse", "main")
			sha := mustGitT(t, box, "rev-parse", body.Ref+"^{commit}")
			if exec.Command("git", "-C", box, "merge-base", "--is-ancestor", before, sha).Run() != nil {
				if exec.Command("git", "-C", box, "merge-base", "--is-ancestor", sha, before).Run() != nil {
					w.WriteHeader(http.StatusConflict)
					_, _ = w.Write([]byte(`{"error":{"code":"BRAIN_NOT_FAST_FORWARD","message":"main has moved since this branch was cut: run rp dev brain pull, rebase"}}`))
					return
				}
				sha = before
			}
			mustGitT(t, box, "update-ref", "refs/heads/main", sha)
			_, _ = fmt.Fprintf(w, `{"project":"alpha","sync":{"before":{"local_sha":%q,"state":"local_only","ref":"main"},"after":{"local_sha":%q,"state":"local_only","ref":"main"},"fast_forwarded":%t}}`,
				before, sha, sha != before)
		})
	}
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func gitIdentityEnv(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{"GIT_AUTHOR_NAME": "dev", "GIT_AUTHOR_EMAIL": "dev@test", "GIT_COMMITTER_NAME": "dev", "GIT_COMMITTER_EMAIL": "dev@test", "GIT_CONFIG_GLOBAL": "/dev/null"} {
		t.Setenv(k, v)
	}
}

// clone → branch + commit → push (dev/<branch>) → publish (main fast-forwards) → server main moves →
// a stale branch is refused, pull fast-forwards local main, a local-only main commit is never discarded.
func TestBrainBundleCloneEditPublishPull(t *testing.T) {
	gitIdentityEnv(t)
	srv, box := fakeBundleServer(t, "none")
	e, out, _ := newTestEnv(t, srv, "table")
	work := t.TempDir()
	t.Chdir(work)

	if err := run(t, e, "dev", "brain", "clone", "alpha"); err != nil {
		t.Fatalf("clone: %v", err)
	}
	co := filepath.Join(work, "alpha")
	if got := mustGitT(t, co, "rev-parse", "main"); got != mustGitT(t, box, "rev-parse", "main") {
		t.Fatalf("clone main %s != box main", got)
	}
	if url := mustGitT(t, co, "remote", "get-url", "origin"); !strings.HasSuffix(url, filepath.Join(".git", bundleFileName)) {
		t.Fatalf("origin = %s, want the bundle in .git", url)
	}
	if marker, err := os.ReadFile(filepath.Join(co, ".replypen.toml")); err != nil || !strings.Contains(string(marker), `project = "alpha"`) {
		t.Fatalf("marker = %q, %v", marker, err)
	}

	t.Chdir(co)
	mustGitT(t, co, "switch", "-q", "-c", "feature/x")
	edit := commitIn(t, co, "skill.md", "skill\n")
	out.Reset()
	if err := run(t, e, "dev", "brain", "push"); err != nil {
		t.Fatalf("push: %v", err)
	}
	if got := mustGitT(t, box, "rev-parse", "dev/feature-x"); got != edit {
		t.Fatalf("box dev/feature-x = %s, want %s", got, edit)
	}
	if !strings.Contains(out.String(), "dev/feature-x") {
		t.Fatalf("push output = %q", out.String())
	}
	if err := run(t, e, "dev", "brain", "publish"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if got := mustGitT(t, box, "rev-parse", "main"); got != edit {
		t.Fatalf("box main = %s, want %s", got, edit)
	}

	// The server's main moves on (a journal commit); a branch cut before it touching other paths is replayed
	// onto it, and the refresh moves local main to the landed commit.
	moved := commitIn(t, box, "journal.md", "j\n")
	mustGitT(t, co, "switch", "-q", "-c", "stale")
	commitIn(t, co, "late.md", "late\n")
	if err := run(t, e, "dev", "brain", "publish"); err != nil {
		t.Fatalf("stale publish: %v", err)
	}
	landed := mustGitT(t, box, "rev-parse", "main")
	if mustGitT(t, box, "rev-parse", "main~1") != moved || mustGitT(t, co, "rev-parse", "HEAD") != landed || mustGitT(t, co, "rev-parse", "main") != landed {
		t.Fatalf("stale publish was not replayed onto %s and refreshed", moved)
	}

	// pull fast-forwards local main even when another branch is checked out.
	moved2 := commitIn(t, box, "journal2.md", "j2\n")
	if err := run(t, e, "dev", "brain", "pull"); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if got := mustGitT(t, co, "rev-parse", "main"); got != moved2 {
		t.Fatalf("local main after pull = %s, want %s", got, moved2)
	}

	// A local main commit the server lacks is never discarded by pull.
	mustGitT(t, co, "switch", "-q", "main")
	local := commitIn(t, co, "local.md", "mine\n")
	commitIn(t, box, "journal3.md", "j3\n")
	out.Reset()
	if err := run(t, e, "dev", "brain", "pull"); err != nil {
		t.Fatalf("pull with local commits: %v", err)
	}
	if got := mustGitT(t, co, "rev-parse", "main"); got != local {
		t.Fatalf("pull discarded a local main commit: main = %s", got)
	}
	if !strings.Contains(out.String(), "left as is") {
		t.Fatalf("pull output = %q", out.String())
	}
}

func TestBrainBundleRefusesGitHubBrain(t *testing.T) {
	gitIdentityEnv(t)
	srv, _ := fakeBundleServer(t, "github")
	e, _, _ := newTestEnv(t, srv, "table")
	t.Chdir(t.TempDir())
	err := run(t, e, "dev", "brain", "clone", "alpha")
	if err == nil || !strings.Contains(err.Error(), "GitHub") {
		t.Fatalf("clone of a github brain err = %v", err)
	}
	if _, serr := os.Stat("alpha"); serr == nil {
		t.Fatal("refused clone left a directory")
	}
}

func TestDevName(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	for in, want := range map[string]string{"feature/x": "feature-x", "main": "edit-0123456789ab", "-x": "x", "é/ü": "edit-0123456789ab", "fix.1": "fix.1"} {
		if got := devName(in, sha); got != want {
			t.Errorf("devName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A tenant checkout (tenant from its .replypen/local.toml, no flag) never publishes to the project brain:
// --channel is refused, and a --scope project override is caught by the checkout's own marker.
func TestBrainBundleTenantCheckoutNeverPublishesProject(t *testing.T) {
	gitIdentityEnv(t)
	f := newFakeBundle(t, "none")
	e, _, _ := newTestEnv(t, f.srv, "table")
	work := t.TempDir()
	t.Chdir(work)
	if err := run(t, e, "dev", "brain", "clone", "alpha", "--tenant", "t1"); err != nil {
		t.Fatalf("clone: %v", err)
	}
	co := filepath.Join(work, "alpha-t1")
	t.Chdir(co)
	mustGitT(t, co, "switch", "-q", "-c", "edit")
	edit := commitIn(t, co, "tenant.md", "t\n")
	projectMain := mustGitT(t, f.box, "rev-parse", "main")

	e2, _, _ := newTestEnv(t, f.srv, "table")
	err := run(t, e2, "dev", "brain", "publish", "--channel", "stable")
	if err == nil || !strings.Contains(err.Error(), "tenant t1") || !strings.Contains(err.Error(), "drop --channel") {
		t.Fatalf("tenant publish --channel err = %v", err)
	}
	e3, _, _ := newTestEnv(t, f.srv, "table")
	err = run(t, e3, "dev", "brain", "publish", "--scope", "project")
	if err == nil || !strings.Contains(err.Error(), "this checkout is the alpha tenant t1 brain") {
		t.Fatalf("tenant checkout publish --scope project err = %v", err)
	}
	if f.uploads[""] != 0 || mustGitT(t, f.box, "rev-parse", "main") != projectMain {
		t.Fatalf("the project brain was written: uploads=%d", f.uploads[""])
	}

	e4, _, _ := newTestEnv(t, f.srv, "table")
	if err := run(t, e4, "dev", "brain", "publish"); err != nil {
		t.Fatalf("tenant publish: %v", err)
	}
	if got := mustGitT(t, f.tenant, "rev-parse", "main"); got != edit {
		t.Fatalf("tenant main = %s, want %s", got, edit)
	}
}

// A bundle that commits .replypen (or a marker) as a symlink must not make clone write outside the checkout.
func TestBrainBundleCloneRefusesSymlinkedMarkers(t *testing.T) {
	gitIdentityEnv(t)
	for _, c := range []struct{ name, link string }{
		{"dir", ".replypen"},
		{"local", ".replypen/local.toml"},
		{"marker", ".replypen.toml"},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFakeBundle(t, "none")
			victim := filepath.Join(t.TempDir(), "victim")
			if err := os.MkdirAll(victim, 0o755); err != nil {
				t.Fatal(err)
			}
			victimFile := filepath.Join(victim, "local.toml")
			if err := os.WriteFile(victimFile, []byte("precious\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			target := victim
			if c.name != "dir" {
				target = victimFile
			}
			if err := os.MkdirAll(filepath.Dir(filepath.Join(f.tenant, c.link)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(f.tenant, c.link)); err != nil {
				t.Fatal(err)
			}
			mustGitT(t, f.tenant, "add", "-A")
			mustGitT(t, f.tenant, "commit", "-q", "-m", "evil link")

			e, _, _ := newTestEnv(t, f.srv, "table")
			work := t.TempDir()
			t.Chdir(work)
			err := run(t, e, "dev", "brain", "clone", "alpha", "--tenant", "t1")
			if err == nil || !strings.Contains(err.Error(), "symlink") {
				t.Fatalf("clone err = %v, want a symlink refusal", err)
			}
			if got, _ := os.ReadFile(victimFile); string(got) != "precious\n" {
				t.Fatalf("clone wrote through the symlink: victim = %q", got)
			}
			if entries, _ := os.ReadDir(victim); len(entries) != 1 {
				t.Fatalf("clone created files outside the checkout: %v", entries)
			}
			if _, serr := os.Stat(filepath.Join(work, "alpha-t1")); serr == nil {
				t.Fatal("the refused clone was left behind")
			}
		})
	}
}

// push refuses a bundle over the server cap before sending, and streams the file (Content-Length set).
func TestBrainBundlePushSizeCapAndStream(t *testing.T) {
	gitIdentityEnv(t)
	f := newFakeBundle(t, "none")
	e, _, _ := newTestEnv(t, f.srv, "table")
	work := t.TempDir()
	t.Chdir(work)
	if err := run(t, e, "dev", "brain", "clone", "alpha"); err != nil {
		t.Fatalf("clone: %v", err)
	}
	co := filepath.Join(work, "alpha")
	t.Chdir(co)
	mustGitT(t, co, "switch", "-q", "-c", "big")
	commitIn(t, co, "big.md", strings.Repeat("x", 4096))

	old := maxBundleUpload
	maxBundleUpload = 64
	e2, _, _ := newTestEnv(t, f.srv, "table")
	err := run(t, e2, "dev", "brain", "push")
	maxBundleUpload = old
	if err == nil || !strings.Contains(err.Error(), "upload limit") {
		t.Fatalf("oversize push err = %v", err)
	}
	if f.uploads[""] != 0 {
		t.Fatal("an oversize bundle was sent")
	}
	e3, _, _ := newTestEnv(t, f.srv, "table")
	if err := run(t, e3, "dev", "brain", "push"); err != nil {
		t.Fatalf("push: %v", err)
	}
	if len(f.contentLength) != 1 || f.contentLength[0] <= 0 {
		t.Fatalf("upload Content-Length = %v, want the file size", f.contentLength)
	}
}

// pull from a feature worktree leaves main alone when another worktree has it checked out.
func TestBrainBundlePullLeavesMainCheckedOutElsewhere(t *testing.T) {
	gitIdentityEnv(t)
	f := newFakeBundle(t, "none")
	e, out, _ := newTestEnv(t, f.srv, "table")
	work := t.TempDir()
	t.Chdir(work)
	if err := run(t, e, "dev", "brain", "clone", "alpha"); err != nil {
		t.Fatalf("clone: %v", err)
	}
	co := filepath.Join(work, "alpha")
	mainBefore := mustGitT(t, co, "rev-parse", "main")
	feature := filepath.Join(work, "feature")
	mustGitT(t, co, "worktree", "add", "-q", "-b", "feature", feature)
	commitIn(t, f.box, "journal.md", "j\n")

	t.Chdir(feature)
	out.Reset()
	if err := run(t, e, "dev", "brain", "pull"); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if got := mustGitT(t, co, "rev-parse", "main"); got != mainBefore {
		t.Fatalf("pull moved main under the worktree that has it checked out: %s", got)
	}
	if !strings.Contains(out.String(), "checked out in") {
		t.Fatalf("pull output = %q", out.String())
	}
	if status := mustGitT(t, co, "status", "--porcelain", "--untracked-files=no"); status != "" {
		t.Fatalf("main worktree went stale: %q", status)
	}
}

func writeIn(t *testing.T, dir, file, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func isolateCache(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	cache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(cache)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

// The throwaway flow on a tenant brain: clone --temp (JSON), dirty push refused, push -m (tenant hint),
// publish -m --rm lands, verifies and deletes the checkout.
func TestBrainBundleTempCloneTenantFlow(t *testing.T) {
	gitIdentityEnv(t)
	cache := isolateCache(t)
	f := newFakeBundle(t, "none")
	t.Chdir(t.TempDir())
	e, out, _ := newTestEnv(t, f.srv, "json")
	if err := run(t, e, "dev", "brain", "clone", "alpha", "x", "--tenant", "t1", "--temp"); err == nil || !strings.Contains(err.Error(), "drop the dir") {
		t.Fatalf("clone --temp with a dir err = %v", err)
	}
	if err := run(t, e, "dev", "brain", "clone", "alpha", "--tenant", "t1", "--temp"); err != nil {
		t.Fatalf("clone --temp: %v", err)
	}
	var cl struct {
		Project, Tenant, Dir, SHA string
		Temp                      bool
	}
	if err := json.Unmarshal(out.Bytes(), &cl); err != nil {
		t.Fatalf("clone receipt %q: %v", out.String(), err)
	}
	oldMain := mustGitT(t, f.tenant, "rev-parse", "main")
	if !cl.Temp || cl.Tenant != "t1" || cl.SHA != oldMain || !strings.HasPrefix(cl.Dir, filepath.Join(cache, "replypen", "brains", "alpha-t1-")) {
		t.Fatalf("clone receipt = %+v", cl)
	}
	co := cl.Dir
	t.Chdir(co)

	writeIn(t, co, "tenant.md", "t\n")
	et, tout, _ := newTestEnv(t, f.srv, "table")
	if err := run(t, et, "dev", "brain", "push"); err == nil || !strings.Contains(err.Error(), "uncommitted changes") {
		t.Fatalf("dirty push err = %v", err)
	}
	if f.uploads["t1"] != 0 {
		t.Fatal("a dirty tree was uploaded")
	}
	if err := run(t, et, "dev", "brain", "push", "-m", "proof"); err != nil {
		t.Fatalf("push -m: %v", err)
	}
	if !strings.Contains(tout.String(), "Test it with --tenant-brain-ref dev/edit-") {
		t.Fatalf("tenant push hint = %q", tout.String())
	}
	if got := mustGitT(t, co, "log", "-1", "--format=%s"); got != "proof" {
		t.Fatalf("push -m commit subject = %q", got)
	}

	writeIn(t, co, "more.md", "m\n")
	ej, jout, _ := newTestEnv(t, f.srv, "json")
	if err := run(t, ej, "dev", "brain", "publish", "-m", "what and why", "--rm"); err != nil {
		t.Fatalf("publish --rm: %v", err)
	}
	var rc struct {
		Project, Tenant, SHA, Dir string
		PreviousMain              string `json:"previous_main"`
		DevRef                    string `json:"dev_ref"`
		Rebased, Removed          bool
	}
	if err := json.Unmarshal(jout.Bytes(), &rc); err != nil {
		t.Fatalf("publish receipt %q: %v", jout.String(), err)
	}
	landed := mustGitT(t, f.tenant, "rev-parse", "main")
	if rc.SHA != landed || rc.PreviousMain != oldMain || rc.Tenant != "t1" || rc.Project != "alpha" || !strings.HasPrefix(rc.DevRef, "dev/edit-") || rc.Rebased || !rc.Removed || rc.Dir != co {
		t.Fatalf("publish receipt = %+v (landed %s)", rc, landed)
	}
	if got := mustGitT(t, f.tenant, "log", "-1", "--format=%s", "main"); got != "what and why" {
		t.Fatalf("landed commit subject = %q", got)
	}
	if _, err := os.Stat(co); !os.IsNotExist(err) {
		t.Fatalf("checkout still there after --rm: %v", err)
	}

	t.Chdir(t.TempDir())
	if err := run(t, et, "dev", "brain", "clone", "alpha", "--tenant", "t1", "--temp"); err != nil {
		t.Fatalf("second clone: %v", err)
	}
	if !strings.Contains(tout.String(), "--tenant-brain-ref dev/<name>") || strings.Contains(tout.String(), "--brain-ref dev/") {
		t.Fatalf("tenant clone hint = %q", tout.String())
	}
}

// main moved upstream: disjoint paths are replayed (rebased: true); overlapping paths are refused with
// the checkout untouched and nothing landed.
func TestBrainBundlePublishMovedMain(t *testing.T) {
	gitIdentityEnv(t)
	f := newFakeBundle(t, "none")
	work := t.TempDir()
	t.Chdir(work)
	e, _, _ := newTestEnv(t, f.srv, "table")
	if err := run(t, e, "dev", "brain", "clone", "alpha"); err != nil {
		t.Fatalf("clone: %v", err)
	}
	co := filepath.Join(work, "alpha")
	t.Chdir(co)

	writeIn(t, co, "skill.md", "mine\n")
	moved := commitIn(t, f.box, "journal.md", "j\n")
	ej, jout, _ := newTestEnv(t, f.srv, "json")
	if err := run(t, ej, "dev", "brain", "publish", "-m", "skill"); err != nil {
		t.Fatalf("disjoint publish: %v", err)
	}
	var rc struct {
		SHA          string
		PreviousMain string `json:"previous_main"`
		Rebased      bool
	}
	if err := json.Unmarshal(jout.Bytes(), &rc); err != nil {
		t.Fatal(err)
	}
	if !rc.Rebased || rc.PreviousMain != moved || rc.SHA != mustGitT(t, f.box, "rev-parse", "main") || mustGitT(t, f.box, "rev-parse", "main~1") != moved {
		t.Fatalf("disjoint publish receipt = %+v", rc)
	}

	writeIn(t, co, "skill.md", "mine again\n")
	mustGitT(t, co, "add", "-A")
	mustGitT(t, co, "commit", "-q", "-m", "local skill")
	head := mustGitT(t, co, "rev-parse", "HEAD")
	boxMain := commitIn(t, f.box, "skill.md", "theirs\n")
	err := run(t, e, "dev", "brain", "publish")
	if err == nil || !strings.Contains(err.Error(), "skill.md") || !strings.Contains(err.Error(), "unchanged") {
		t.Fatalf("overlap publish err = %v", err)
	}
	if hintFor(err) == "" || exitCodeFor(err) == 0 {
		t.Fatalf("overlap error lacks a next step or exits 0: %v", err)
	}
	var ce *commandError
	if !errors.As(err, &ce) || ce.name != "BRAIN_MAIN_MOVED_OVERLAP" {
		t.Fatalf("overlap error is not typed: %#v", err)
	}
	if mustGitT(t, co, "rev-parse", "HEAD") != head || mustGitT(t, co, "status", "--porcelain") != "" || mustGitT(t, f.box, "rev-parse", "main") != boxMain {
		t.Fatal("overlap publish changed the checkout or the box")
	}
}

// A main that keeps moving gives up after three land attempts, landing nothing.
func TestBrainBundlePublishRetryBound(t *testing.T) {
	gitIdentityEnv(t)
	f := newFakeBundle(t, "none")
	work := t.TempDir()
	t.Chdir(work)
	e, _, _ := newTestEnv(t, f.srv, "table")
	if err := run(t, e, "dev", "brain", "clone", "alpha"); err != nil {
		t.Fatalf("clone: %v", err)
	}
	co := filepath.Join(work, "alpha")
	t.Chdir(co)
	writeIn(t, co, "skill.md", "mine\n")
	n := 0
	f.beforeSync = func(box string) {
		n++
		commitIn(t, box, fmt.Sprintf("journal%d.md", n), "j\n")
	}
	err := run(t, e, "dev", "brain", "publish", "-m", "skill")
	if err == nil || !strings.Contains(err.Error(), "after 3 attempts") {
		t.Fatalf("retry err = %v", err)
	}
	if f.syncs != 3 || f.uploads[""] != 3 {
		t.Fatalf("syncs=%d uploads=%d, want 3 each", f.syncs, f.uploads[""])
	}
	if strings.Contains(mustGitT(t, f.box, "log", "--format=%s", "main"), "skill") {
		t.Fatal("a commit landed despite the refusal")
	}
}

// --rm never deletes a checkout rp did not clone, and keeps one that still holds unpublished work.
func TestBrainBundlePublishRmSafety(t *testing.T) {
	gitIdentityEnv(t)
	isolateCache(t)
	f := newFakeBundle(t, "none")
	t.Chdir(t.TempDir())
	ej, jout, _ := newTestEnv(t, f.srv, "json")
	if err := run(t, ej, "dev", "brain", "clone", "alpha", "--temp"); err != nil {
		t.Fatalf("clone: %v", err)
	}
	var cl struct{ Dir string }
	if err := json.Unmarshal(jout.Bytes(), &cl); err != nil {
		t.Fatal(err)
	}
	co := cl.Dir
	t.Chdir(co)

	bundleURL := mustGitT(t, co, "remote", "get-url", "origin")
	mustGitT(t, co, "remote", "set-url", "origin", "https://github.com/acme/brain.git")
	writeIn(t, co, "skill.md", "s\n")
	e, _, _ := newTestEnv(t, f.srv, "table")
	err := run(t, e, "dev", "brain", "publish", "-m", "skill", "--rm")
	if err == nil || !strings.Contains(err.Error(), "not one") {
		t.Fatalf("--rm on a non-bundle checkout err = %v", err)
	}
	if _, serr := os.Stat(co); serr != nil || f.uploads[""] != 0 {
		t.Fatalf("refused --rm touched something: stat=%v uploads=%d", serr, f.uploads[""])
	}

	mustGitT(t, co, "remote", "set-url", "origin", bundleURL)
	mustGitT(t, co, "branch", "side")
	commitIn(t, co, "side.md", "x\n")
	mustGitT(t, co, "switch", "-q", "side")
	commitIn(t, co, "side2.md", "y\n")
	mustGitT(t, co, "switch", "-q", "main")
	ek, kout, kerr := newTestEnv(t, f.srv, "json")
	if err := run(t, ek, "dev", "brain", "publish", "-m", "skill", "--rm"); err != nil {
		t.Fatalf("publish --rm with an unpublished branch: %v", err)
	}
	var kept struct{ Removed bool }
	if err := json.Unmarshal(kout.Bytes(), &kept); err != nil || kept.Removed || !strings.Contains(kerr.String(), co) || !strings.Contains(kerr.String(), "branch side") {
		t.Fatalf("kept receipt = %q, stderr = %q", kout.String(), kerr.String())
	}
	if _, serr := os.Stat(co); serr != nil {
		t.Fatalf("checkout with unpublished work was deleted: %v", serr)
	}
}

// tempCheckout clones the project brain with --temp and chdirs into it.
func tempCheckout(t *testing.T, f *fakeBundle) string {
	t.Helper()
	isolateCache(t)
	t.Chdir(t.TempDir())
	e, out, _ := newTestEnv(t, f.srv, "json")
	if err := run(t, e, "dev", "brain", "clone", "alpha", "--temp"); err != nil {
		t.Fatalf("clone: %v", err)
	}
	var cl struct{ Dir string }
	if err := json.Unmarshal(out.Bytes(), &cl); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cl.Dir)
	return cl.Dir
}

// publishRmKept runs publish -m --rm, which must land but keep the checkout for reason.
func publishRmKept(t *testing.T, f *fakeBundle, co, reason string) {
	t.Helper()
	e, out, errb := newTestEnv(t, f.srv, "json")
	if err := run(t, e, "dev", "brain", "publish", "-m", "edit", "--rm"); err != nil {
		t.Fatalf("publish --rm: %v", err)
	}
	var rc struct{ Removed bool }
	if err := json.Unmarshal(out.Bytes(), &rc); err != nil || rc.Removed || !strings.Contains(errb.String(), co) || !strings.Contains(errb.String(), reason) {
		t.Fatalf("receipt = %q, stderr = %q", out.String(), errb.String())
	}
	if _, err := os.Stat(co); err != nil {
		t.Fatalf("checkout was deleted: %v", err)
	}
}

func TestBrainBundlePublishRmKeepsIgnoredWork(t *testing.T) {
	gitIdentityEnv(t)
	f := newFakeBundle(t, "none")
	co := tempCheckout(t, f)
	writeIn(t, co, ".gitignore", "notes.txt\n")
	writeIn(t, co, "notes.txt", "precious\n")
	publishRmKept(t, f, co, "ignored files")
	if got, _ := os.ReadFile(filepath.Join(co, "notes.txt")); string(got) != "precious\n" {
		t.Fatalf("ignored work lost: %q", got)
	}
}

func TestBrainBundlePublishRmKeepsLinkedWorktree(t *testing.T) {
	gitIdentityEnv(t)
	f := newFakeBundle(t, "none")
	co := tempCheckout(t, f)
	wt := filepath.Join(t.TempDir(), "wt")
	mustGitT(t, co, "worktree", "add", "-q", "--detach", wt)
	commitIn(t, wt, "wip.md", "wip\n")
	writeIn(t, co, "skill.md", "s\n")
	publishRmKept(t, f, co, "worktrees")
}

// A local merge commit is never auto-replayed (rebase --onto would drop its own changes).
func TestBrainBundlePublishRefusesReplayingMerge(t *testing.T) {
	gitIdentityEnv(t)
	f := newFakeBundle(t, "none")
	co := tempCheckout(t, f)
	mustGitT(t, co, "switch", "-q", "-c", "side")
	commitIn(t, co, "a.md", "a\n")
	mustGitT(t, co, "switch", "-q", "main")
	commitIn(t, co, "b.md", "b\n")
	mustGitT(t, co, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	writeIn(t, co, "a.md", "fixed in merge\n")
	mustGitT(t, co, "commit", "-q", "--amend", "-a", "--no-edit")
	head := mustGitT(t, co, "rev-parse", "HEAD")
	boxMain := commitIn(t, f.box, "journal.md", "j\n")
	e, _, _ := newTestEnv(t, f.srv, "table")
	err := run(t, e, "dev", "brain", "publish")
	var ce *commandError
	if err == nil || !strings.Contains(err.Error(), "merge") || !errors.As(err, &ce) || ce.name != "BRAIN_MAIN_MOVED_OVERLAP" {
		t.Fatalf("merge replay err = %v", err)
	}
	if mustGitT(t, co, "rev-parse", "HEAD") != head || mustGitT(t, f.box, "rev-parse", "main") != boxMain {
		t.Fatal("refused merge replay changed the checkout or the box")
	}
}

// Disjoint paths that still conflict (file a vs directory a/) abort the rebase: checkout back as it was.
func TestBrainBundlePublishRebaseConflictAborts(t *testing.T) {
	gitIdentityEnv(t)
	f := newFakeBundle(t, "none")
	co := tempCheckout(t, f)
	writeIn(t, co, "a", "file\n")
	mustGitT(t, co, "add", "a")
	mustGitT(t, co, "commit", "-q", "-m", "file a")
	head := mustGitT(t, co, "rev-parse", "HEAD")
	if err := os.MkdirAll(filepath.Join(f.box, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	boxMain := commitIn(t, f.box, "a/b", "dir\n")
	e, _, _ := newTestEnv(t, f.srv, "table")
	err := run(t, e, "dev", "brain", "publish")
	if err == nil || !strings.Contains(err.Error(), "conflicted") {
		t.Fatalf("conflicting replay err = %v", err)
	}
	gitDir := mustGitT(t, co, "rev-parse", "--absolute-git-dir")
	for _, d := range []string{"rebase-merge", "rebase-apply"} {
		if _, serr := os.Stat(filepath.Join(gitDir, d)); serr == nil {
			t.Fatalf("rebase left in progress (%s)", d)
		}
	}
	if mustGitT(t, co, "rev-parse", "HEAD") != head || mustGitT(t, co, "symbolic-ref", "--short", "HEAD") != "main" ||
		mustGitT(t, co, "status", "--porcelain", "--untracked-files=no") != "" || mustGitT(t, f.box, "rev-parse", "main") != boxMain {
		t.Fatal("aborted replay did not restore the checkout, or touched the box")
	}
}

// Overlap is detected on paths git would quote (spaces, non-ASCII).
func TestBrainBundlePublishOverlapOddPaths(t *testing.T) {
	gitIdentityEnv(t)
	for _, name := range []string{"my file.md", "café.md"} {
		t.Run(name, func(t *testing.T) {
			f := newFakeBundle(t, "none")
			co := tempCheckout(t, f)
			commitIn(t, co, name, "mine\n")
			boxMain := commitIn(t, f.box, name, "theirs\n")
			e, _, _ := newTestEnv(t, f.srv, "table")
			err := run(t, e, "dev", "brain", "publish")
			if err == nil || !strings.Contains(err.Error(), "same paths as your commits: "+name) {
				t.Fatalf("overlap on %q err = %v", name, err)
			}
			if mustGitT(t, f.box, "rev-parse", "main") != boxMain {
				t.Fatal("an overlapping change landed")
			}
		})
	}
}

// branch → pull → publish: the replay base is the branch's merge-base with the new main, not the stored
// origin/main, so paths the box changed before the pull are not mistaken for local changes.
func TestBrainBundlePublishReplayAfterPull(t *testing.T) {
	gitIdentityEnv(t)
	f := newFakeBundle(t, "none")
	co := tempCheckout(t, f)
	mustGitT(t, co, "switch", "-q", "-c", "feature")
	commitIn(t, co, "local.md", "mine\n")
	commitIn(t, f.box, "journal.md", "j1\n")
	e, _, _ := newTestEnv(t, f.srv, "table")
	if err := run(t, e, "dev", "brain", "pull"); err != nil {
		t.Fatalf("pull: %v", err)
	}
	moved := commitIn(t, f.box, "journal.md", "j2\n")
	if err := run(t, e, "dev", "brain", "publish"); err != nil {
		t.Fatalf("publish after pull: %v", err)
	}
	if mustGitT(t, f.box, "rev-parse", "main~1") != moved || mustGitT(t, f.box, "show", "main:local.md") != "mine" {
		t.Fatal("the branch was not replayed onto the moved main")
	}
}

// A detached HEAD is refused before -m commits, so no commit is stranded.
func TestBrainBundleDetachedDirtyRefusedBeforeCommit(t *testing.T) {
	gitIdentityEnv(t)
	f := newFakeBundle(t, "none")
	co := tempCheckout(t, f)
	mustGitT(t, co, "switch", "-q", "--detach")
	head := mustGitT(t, co, "rev-parse", "HEAD")
	writeIn(t, co, "skill.md", "s\n")
	e, _, _ := newTestEnv(t, f.srv, "table")
	for _, cmd := range []string{"push", "publish"} {
		err := run(t, e, "dev", "brain", cmd, "-m", "edit")
		if err == nil || !strings.Contains(err.Error(), "detached") {
			t.Fatalf("%s err = %v", cmd, err)
		}
	}
	if mustGitT(t, co, "rev-parse", "HEAD") != head || !strings.Contains(mustGitT(t, co, "status", "--porcelain"), "skill.md") {
		t.Fatal("a refused upload committed the edit")
	}
}

package cli

import (
	"encoding/json"
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

	// The server's main moves on (a journal commit); a branch cut before it is refused in plain words.
	moved := commitIn(t, box, "journal.md", "j\n")
	mustGitT(t, co, "switch", "-q", "-c", "stale")
	commitIn(t, co, "late.md", "late\n")
	err := run(t, e, "dev", "brain", "publish")
	if err == nil || !strings.Contains(err.Error(), "pull") {
		t.Fatalf("stale publish err = %v", err)
	}
	if got := mustGitT(t, box, "rev-parse", "main"); got != moved {
		t.Fatal("stale publish moved main")
	}

	// pull fast-forwards local main even when another branch is checked out.
	if err := run(t, e, "dev", "brain", "pull"); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if got := mustGitT(t, co, "rev-parse", "main"); got != moved {
		t.Fatalf("local main after pull = %s, want %s", got, moved)
	}
	mustGitT(t, co, "rebase", "-q", "main")
	if err := run(t, e, "dev", "brain", "publish"); err != nil {
		t.Fatalf("publish after rebase: %v", err)
	}

	// A local main commit the server lacks is never discarded by pull.
	mustGitT(t, co, "switch", "-q", "main")
	mustGitT(t, co, "merge", "-q", "--ff-only", "stale")
	local := commitIn(t, co, "local.md", "mine\n")
	commitIn(t, box, "journal2.md", "j2\n")
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

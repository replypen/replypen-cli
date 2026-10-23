package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/replypen/replypen-cli/internal/client"
)

// mirrorTipServer answers every refresh whose expected_sha != tip with the host's 409
// MIRROR_SHA_MISMATCH (details expected_sha/actual_sha), and verifies at the tip otherwise.
func mirrorTipServer(t *testing.T, tip string, calls *[]string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"id":"aaaaaaaa-0000-0000-0000-000000000001","name":"alpha"}]}`))
	})
	mux.HandleFunc("GET /api/v1/whoami", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"email":"dev@example.test","project":{"id":"aaaaaaaa-0000-0000-0000-000000000001","name":"alpha"}}`))
	})
	mux.HandleFunc("POST /api/v1/projects/{project}/mirrors/refresh", func(w http.ResponseWriter, r *http.Request) {
		var body client.MirrorRefreshRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		*calls = append(*calls, body.ExpectedSHA)
		w.Header().Set("Content-Type", "application/json")
		if body.ExpectedSHA != tip {
			w.WriteHeader(http.StatusConflict)
			_, _ = fmt.Fprintf(w, `{"error":{"code":"MIRROR_SHA_MISMATCH","message":"mirror did not reach the expected commit","details":[{"field":"expected_sha","message":%q},{"field":"actual_sha","message":%q}]}}`, body.ExpectedSHA, tip)
			return
		}
		_, _ = fmt.Fprintf(w, `{"project":"alpha","repo":"common","branch":"stable","expected_sha":%q,"actual_sha":%q,"verified":true,"job_id":7,"refreshed_workspaces":2}`, tip, tip)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// tempGitRepo builds base → tip on main plus a sibling commit off base, and chdirs into it.
func tempGitRepo(t *testing.T) (base, tip, sibling string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	git := func(args ...string) string {
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	commit := func(msg string) string {
		git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", msg)
		return git("rev-parse", "HEAD")
	}
	base = commit("base")
	tip = commit("tip")
	git("checkout", "-q", "-b", "side", base)
	sibling = commit("sibling")
	return base, tip, sibling
}

func TestMirrorRefreshAcceptDescendant(t *testing.T) {
	base, tip, sibling := tempGitRepo(t)

	t.Run("ancestor re-verifies at the new tip", func(t *testing.T) {
		var calls []string
		e := newTestEnvAt(t, mirrorTipServer(t, tip, &calls).URL, "table")
		var stdout bytes.Buffer
		e.out = &stdout
		if err := run(t, e, "--project", "alpha", "dev", "mirror", "refresh", "--repo", "common", "--expect-sha", base, "--accept-descendant"); err != nil {
			t.Fatalf("refresh: %v", err)
		}
		if strings.Join(calls, ",") != base+","+tip {
			t.Fatalf("calls = %v, want expected then actual", calls)
		}
		out := stdout.String()
		if !strings.Contains(out, "Tip moved:  "+base+" → "+tip) || !strings.Contains(out, "Verified:   yes") {
			t.Fatalf("output:\n%s", out)
		}
	})

	t.Run("non-ancestor keeps the 409", func(t *testing.T) {
		var calls []string
		e := newTestEnvAt(t, mirrorTipServer(t, tip, &calls).URL, "table")
		err := run(t, e, "--project", "alpha", "dev", "mirror", "refresh", "--repo", "common", "--expect-sha", sibling, "--accept-descendant")
		if _, ok := mirrorMismatchActual(err); !ok || len(calls) != 1 {
			t.Fatalf("err = %v calls = %v, want the original 409 and no retry", err, calls)
		}
		if h := hintFor(err); !strings.Contains(h, "not an ancestor") || !strings.Contains(h, sibling[:12]) || !strings.Contains(h, tip[:12]) {
			t.Fatalf("hint = %q", h)
		}
	})

	t.Run("without the flag the 409 suggests it", func(t *testing.T) {
		var calls []string
		e := newTestEnvAt(t, mirrorTipServer(t, tip, &calls).URL, "table")
		err := run(t, e, "--project", "alpha", "dev", "mirror", "refresh", "--repo", "common", "--expect-sha", base)
		if _, ok := mirrorMismatchActual(err); !ok || !strings.Contains(hintFor(err), "--accept-descendant") || len(calls) != 1 {
			t.Fatalf("err = %v hint = %q calls = %v", err, hintFor(err), calls)
		}
	})
}

func TestMirrorRefreshAcceptDescendantOutsideGitRepo(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("GIT_CEILING_DIRECTORIES", t.TempDir())
	tip := strings.Repeat("b", 40)
	var calls []string
	e := newTestEnvAt(t, mirrorTipServer(t, tip, &calls).URL, "table")
	err := run(t, e, "--project", "alpha", "dev", "mirror", "refresh", "--repo", "common", "--expect-sha", strings.Repeat("a", 40), "--accept-descendant")
	if _, ok := mirrorMismatchActual(err); !ok || len(calls) != 1 || !strings.Contains(hintFor(err), "pull/rebase onto the new tip") {
		t.Fatalf("err = %v hint = %q calls = %v", err, hintFor(err), calls)
	}
}

// mirrorPublishFixture: a checkout whose origin has main at tip and stable at stableTip, and a server
// where alpha mirrors "common" on main, beta on stable, gamma mirrors something else.
func mirrorPublishFixture(t *testing.T, stableAtTip bool) (srv *httptest.Server, base, tip string, refreshed map[string]string) {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) string {
		out, err := exec.Command("git", args...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	origin := dir + "/common.git"
	git("init", "-q", "--bare", "-b", "main", origin)
	t.Chdir(dir)
	git("clone", "-q", origin, "work")
	t.Chdir(dir + "/work")
	commit := func(msg string) string {
		git("-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", msg)
		return git("rev-parse", "HEAD")
	}
	base = commit("base")
	tip = commit("tip")
	stable := base
	if stableAtTip {
		stable = tip
	}
	git("push", "-q", "origin", "main", stable+":refs/heads/stable")

	refreshed = map[string]string{}
	branches := map[string]string{"alpha": "main", "beta": "stable"}
	originURL := git("remote", "get-url", "origin")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"id":"1","name":"alpha"},{"id":"2","name":"beta"},{"id":"3","name":"gamma"},{"id":"4","name":"delta"}]}`))
	})
	mux.HandleFunc("GET /api/v1/projects/{project}/repos", func(w http.ResponseWriter, r *http.Request) {
		p := r.PathValue("project")
		if b, ok := branches[p]; ok {
			_, _ = fmt.Fprintf(w, `{"repos":[{"name":"common","default_branch":%q,"git_url":%q}]}`, b, originURL)
			return
		}
		if p == "delta" { // same mirror name, different repository: must never be refreshed
			_, _ = w.Write([]byte(`{"repos":[{"name":"common","default_branch":"main","git_url":"https://github.com/someone/common.git"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"repos":[{"name":"other","default_branch":"main"}]}`))
	})
	mux.HandleFunc("POST /api/v1/projects/{project}/mirrors/refresh", func(w http.ResponseWriter, r *http.Request) {
		var body client.MirrorRefreshRequest
		_ = json.NewDecoder(r.Body).Decode(&body)
		p := r.PathValue("project")
		refreshed[p] = body.ExpectedSHA
		_, _ = fmt.Fprintf(w, `{"project":%q,"repo":%q,"branch":%q,"expected_sha":%q,"actual_sha":%q,"verified":true,"future_field":true}`, p, body.Repo, branches[p], body.ExpectedSHA, body.ExpectedSHA)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, base, tip, refreshed
}

func TestMirrorPublishRefreshesEveryConsumerAtItsBranchTip(t *testing.T) {
	srv, base, _, refreshed := mirrorPublishFixture(t, false)
	e, out, _ := newTestEnv(t, srv, "table")
	// base is on main (ancestor of tip) and is stable's tip: each mirror verifies at its own branch tip.
	if err := run(t, e, "dev", "mirror", "publish", "--sha", base); err != nil {
		t.Fatalf("publish: %v", err)
	}
	tip := refreshed["alpha"]
	if refreshed["beta"] != base || tip == base || len(refreshed) != 2 {
		t.Fatalf("refreshed = %v", refreshed)
	}
	if !strings.Contains(out.String(), "LIVE  beta") {
		t.Fatalf("out = %q", out.String())
	}
}

// TestMirrorPublishJSONCarriesRawRefreshBodies: -o json keeps each refresh body verbatim under
// raw["refresh:<project>"], so a host field the client does not model is not lost.
func TestMirrorPublishJSONCarriesRawRefreshBodies(t *testing.T) {
	srv, base, _, _ := mirrorPublishFixture(t, false)
	e, out, _ := newTestEnv(t, srv, "json")
	if err := run(t, e, "dev", "mirror", "publish", "--sha", base); err != nil {
		t.Fatalf("publish: %v", err)
	}
	raw := rawSources(t, out.Bytes(), "refresh:alpha", "refresh:beta")
	if raw["refresh:alpha"]["project"] != "alpha" || raw["refresh:beta"]["future_field"] != true {
		t.Fatalf("raw = %v", raw)
	}
}

func TestMirrorPublishRefusesWhenATrackedBranchLacksTheSHA(t *testing.T) {
	srv, _, tip, refreshed := mirrorPublishFixture(t, false)
	e, _, _ := newTestEnv(t, srv, "table")
	err := run(t, e, "dev", "mirror", "publish", "--sha", tip)
	if err == nil || !strings.Contains(err.Error(), "stable") || len(refreshed) != 0 {
		t.Fatalf("err = %v, refreshed = %v", err, refreshed)
	}
}

func TestRepoIdentity(t *testing.T) {
	want := "github.com/kampadmin/common"
	for _, u := range []string{"https://github.com/kampadmin/common.git", "git@github.com:kampadmin/common.git", "ssh://git@github.com/kampadmin/common", "https://x-access@GitHub.com/kampadmin/common/"} {
		if got := repoIdentity(u); got != want {
			t.Errorf("repoIdentity(%q) = %q", u, got)
		}
	}
}

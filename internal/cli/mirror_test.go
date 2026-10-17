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

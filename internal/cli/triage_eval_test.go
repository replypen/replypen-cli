package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The candidate must be graded on the baseline's exact cases commit, and the rendered card must put the
// regression (a false skip) first with both revisions pinned.
func TestTriageEvalBaselineCompare(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		if r.URL.Path == "/api/v1/projects" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"projects":[{"id":"p1","name":"acme"}]}`))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/projects/acme/triage/eval" {
			http.NotFound(w, r) // incidental reads (whoami hints) are not this command's contract
			return
		}
		body := readBody(t, r)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(body, `"brain_ref":"dev/shorter"`) {
			_, _ = w.Write(fixture(t, "triage_eval_candidate.json"))
			return
		}
		_, _ = w.Write(fixture(t, "triage_eval_baseline.json"))
	}))
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "--project", "acme", "project", "triage", "eval", "--brain-ref", "dev/shorter", "--baseline", "HEAD"); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(bodies) != 2 || !strings.Contains(bodies[0], `"brain_ref":"HEAD"`) ||
		!strings.Contains(bodies[1], `"cases_ref":"1111111111111111111111111111111111111111"`) {
		t.Fatalf("request bodies = %q", bodies)
	}
	assertGolden(t, "triage_eval_compare.golden", out.String())
}

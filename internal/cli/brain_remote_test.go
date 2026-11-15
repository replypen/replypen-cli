package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/replypen/replypen-cli/internal/client"
)

// rp admin project brain-remote posts mode + dry_run and prints the checklist; a refusal prints each
// failed check.
func TestAdminBrainRemote(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/admin/projects/alpha/brain-remote" {
			http.NotFound(w, r)
			return
		}
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		bodies = append(bodies, b)
		w.Header().Set("Content-Type", "application/json")
		if b["dry_run"] == true {
			_, _ = w.Write([]byte(`{"project":"alpha","from":"github","to":"none","dry_run":true,"brains":["alpha brain"]}`))
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":{"code":"BRAIN_REMOTE_REFUSED","message":"brain remote switch refused; nothing was changed","details":[{"message":"alpha brain: main differs from origin (1 ahead, 0 behind)","status":"failed"}]}}`))
	}))
	defer srv.Close()

	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "admin", "project", "brain-remote", "alpha", "--mode", "none", "--dry-run"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "alpha: github → none") || !strings.Contains(out.String(), "dry run, nothing changed") {
		t.Fatalf("dry-run render = %q", out.String())
	}
	err := run(t, e, "admin", "project", "brain-remote", "alpha", "--mode", "none")
	var apiErr *client.APIError
	if !asAPIError(err, &apiErr) || apiErr.Code != "BRAIN_REMOTE_REFUSED" {
		t.Fatalf("refusal err = %v", err)
	}
	var errb strings.Builder
	printError(&errb, apiErr)
	if !strings.Contains(errb.String(), "✗ alpha brain: main differs from origin (1 ahead, 0 behind)") {
		t.Fatalf("stderr = %q", errb.String())
	}
	if len(bodies) != 2 || bodies[0]["mode"] != "none" || bodies[1]["dry_run"] != false {
		t.Fatalf("bodies = %v", bodies)
	}
}

// A local-only brain whose clone is dirty reports another state than local_only; publish still keys on
// the declared mode and never calls sync (which would answer 409 BRAIN_LOCAL_ONLY).
func TestBrainPublishLocalOnlyDirtySkipsSync(t *testing.T) {
	status := fmt.Sprintf(`{"project":"alpha","status":{"available":true,"ref":"main","state":"dirty","remote_mode":"none","channels":[{"channel":"stable","resolved_sha":%q,"matches_origin":false,"state":"current"}]}}`, publishSHA)
	srv, seen := publishStub(t, publishSyncOK, status, "")
	e, _, _ := newTestEnv(t, srv, "table")
	_ = run(t, e, "dev", "brain", "publish", "--channel", "stable", "--sha", publishSHA)
	if seen.sync {
		t.Fatalf("publish called sync on a local-only brain: %+v", *seen)
	}
}

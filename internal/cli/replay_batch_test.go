package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// A batch replay submits ids × reps from one call and returns every new run id; `rp run wait` then polls
// them together and prints the same bodies `rp run show -o json` would.
func TestAskReplayBatchAndRunWait(t *testing.T) {
	const a, b = "aaaaaaaa-0000-0000-0000-000000000001", "bbbbbbbb-0000-0000-0000-000000000002"
	var mu sync.Mutex
	var submitted []string
	polls := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		switch {
		case r.Method == http.MethodPost:
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["simulation"] != true || body["brain_ref"] != "dev/x" {
				t.Errorf("batch body = %v", body)
			}
			run := fmt.Sprintf("r%d", len(submitted))
			submitted = append(submitted, body["replay_run"].(string))
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"run_id":"` + run + `","status":"running","poll_after_ms":1}`))
		case id == "aaaaaaaa" || id == a:
			_, _ = w.Write([]byte(`{"run_id":"` + a + `","status":"done","kind":"chat"}`))
		case id == b:
			http.Error(w, `{"error":{"code":"UNKNOWN_RUN","message":"unknown run"}}`, http.StatusNotFound)
		default:
			polls[id]++
			_, _ = w.Write([]byte(`{"run_id":"` + id + `","status":"done","outcome":"answered"}`))
		}
	}))
	defer srv.Close()

	e, out, _ := newTestEnv(t, srv, "json")
	// The unknown id comes first: a bad id must fail its own rows, never abort the batch.
	err := run(t, e, "ask", "--replay-run", b+",aaaaaaaa", "--reps", "2", "--brain-ref", "dev/x", "--no-wait")
	if err == nil || !strings.Contains(err.Error(), "2 of 4 replay submits failed") {
		t.Fatalf("want the unknown id's two reps reported as failed, got %v", err)
	}
	var rows []replaySubmission
	if jerr := json.Unmarshal(out.Bytes(), &rows); jerr != nil {
		t.Fatalf("decode %q: %v", out.String(), jerr)
	}
	if len(rows) != 4 || rows[0].Status != "error" || rows[1].Status != "error" || rows[2].ReplayOf != a ||
		rows[2].RunID != "r0" || rows[3].Rep != 1 || rows[3].RunID != "r1" || len(submitted) != 2 {
		t.Fatalf("rows = %+v submitted = %v", rows, submitted)
	}

	e, out, _ = newTestEnv(t, srv, "json")
	if err := run(t, e, "run", "wait", "r0", "r1"); err != nil {
		t.Fatalf("run wait: %v", err)
	}
	var got []map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || len(got) != 2 || got[1]["run_id"] != "r1" || got[1]["outcome"] != "answered" {
		t.Fatalf("wait output %s (%v)", out.String(), err)
	}

	if err := run(t, e, "ask", "--replay-run", a, "--reps", "2"); err == nil || !strings.Contains(err.Error(), "--no-wait") {
		t.Fatalf("a batch without --no-wait must refuse, got %v", err)
	}
}

package cli

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// feedbackRoutes serves both feedback planes and records which one each call hit, so a test can prove
// `list` and `<run-id>` never shadow each other.
func feedbackRoutes(t *testing.T, hits *[]string, gotQuery *url.Values) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/feedback", func(w http.ResponseWriter, r *http.Request) {
		*hits = append(*hits, "list")
		*gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`{"feedback":[]}`))
	})
	mux.HandleFunc("POST /api/v1/runs/{id}/feedback", func(w http.ResponseWriter, r *http.Request) {
		*hits = append(*hits, "write:"+r.PathValue("id"))
		_, _ = w.Write([]byte(`{"recorded":true}`))
	})
	return httptest.NewServer(mux)
}

func TestRunFeedbackListQuery(t *testing.T) {
	var hits []string
	var q url.Values
	srv := feedbackRoutes(t, &hits, &q)
	defer srv.Close()
	e, _, _ := newTestEnv(t, srv, "json")

	if err := run(t, e, "run", "feedback", "list", "--score", "negative", "--kind", "chat", "--surface", "embed",
		"--days", "7", "--since", "2026-10-16", "--until", "2026-10-17T00:00:00Z", "--limit", "20", "--before", "run-9"); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"score": "negative", "kind": "chat", "surface": "embed", "days": "7",
		"since": "2026-10-16", "until": "2026-10-17T00:00:00Z", "limit": "20", "before": "run-9"}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("query %s = %q, want %q (full: %s)", k, q.Get(k), v, q.Encode())
		}
	}

	if err := run(t, e, "run", "feedback", "list"); err != nil {
		t.Fatal(err)
	}
	if len(q) != 0 {
		t.Errorf("no-flag call sent query %q, want none (server owns the defaults)", q.Encode())
	}
}

// The `list` subcommand must not swallow the write verb: a run id still routes to the POST.
func TestRunFeedbackWriteStillRoutes(t *testing.T) {
	var hits []string
	var q url.Values
	srv := feedbackRoutes(t, &hits, &q)
	defer srv.Close()
	e, _, _ := newTestEnv(t, srv, "table")

	id := "11111111-1111-1111-1111-111111111111"
	if err := run(t, e, "run", "feedback", id, "--score", "1"); err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0] != "write:"+id {
		t.Fatalf("hits = %v, want one POST for %s", hits, id)
	}
}

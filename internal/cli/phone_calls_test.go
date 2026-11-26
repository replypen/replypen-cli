package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func phoneCallsServer(t *testing.T) *httptest.Server {
	t.Helper()
	const runA, runB, runC = "fd93d291-db37-5c89-a669-c809a31b455e", "fd93aaaa-0000-0000-0000-000000000000", "d823f768-a923-556d-bc41-49f1b0f58632"
	details := map[string]string{
		runA: `{"project":"dentai-phone-nl","tenant":"demo","run_id":"` + runA + `","created_at":"2026-11-26T12:56:43Z","ended_at":"2026-11-26T12:58:54Z","caller":{"phone":"+32485683991"},"events":[` +
			`{"t":5000,"kind":"transcript","who":"agent","text":"Waarmee kan ik u helpen?"},` +
			`{"t":9000,"kind":"transcript","who":"caller","text":"Verplaatsen."},` +
			`{"t":10000,"kind":"tool","status":"done","outcome":"ok","exit_code":0,"cmd":"python /brain/skills/phone/scripts/move.py --yes","stdout":"{\"status\": \"moved\", \"label\": \"maandag 30 november om 14 uur\"}"}]}`,
		runB: `{"project":"dentai-phone-nl","tenant":"demo","run_id":"` + runB + `","created_at":"2026-11-26T11:00:00Z","caller":{"phone":"+32460231429"},"events":[]}`,
		runC: `{"project":"dentai-phone-nl","tenant":"demo","run_id":"` + runC + `","created_at":"2026-11-20T10:00:00Z","caller":{},"events":[]}`,
	}
	list := `{"items":[{"run_id":"` + runA + `","tenant":"demo","created_at":"2026-11-26T12:56:43Z","ended_at":"2026-11-26T12:58:54Z"},` +
		`{"run_id":"` + runB + `","tenant":"demo","created_at":"2026-11-26T11:00:00Z"},` +
		`{"run_id":"` + runC + `","tenant":"demo","created_at":"2026-11-20T10:00:00Z"}],"next_cursor":""}`
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"id":"a","name":"dentai-phone-nl"}]}`))
	})
	mux.HandleFunc("GET /api/v1/projects/dentai-phone-nl/phone-lab/runs", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("tenant") != "demo" {
			t.Errorf("tenant filter not forwarded: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(list))
	})
	mux.HandleFunc("GET /api/v1/projects/dentai-phone-nl/phone-lab/runs/{id}", func(w http.ResponseWriter, r *http.Request) {
		body, ok := details[r.PathValue("id")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	})
	return httptest.NewServer(mux)
}

func TestRunCallsSinceCutsRowsAndRaw(t *testing.T) {
	srv := phoneCallsServer(t)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "json")
	if err := run(t, e, "run", "calls", "--project", "dentai-phone-nl", "--tenant", "demo", "--since", "2026-11-25T00:00:00Z", "--raw-output"); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Calls []struct {
			RunID           string `json:"run_id"`
			Caller, Outcome string
			Turns           int `json:"caller_turns"`
			DurationS       int `json:"duration_s"`
		} `json:"calls"`
		Raw struct {
			Runs struct {
				Items []map[string]any `json:"items"`
			} `json:"phone_lab_runs"`
			Details map[string]map[string]any `json:"phone_lab_run_details"`
		} `json:"raw"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(got.Calls) != 2 || len(got.Raw.Runs.Items) != 2 || len(got.Raw.Details) != 2 || got.Raw.Details[got.Calls[0].RunID]["events"] == nil {
		t.Fatalf("--since must cut both views to the 2 newer calls: %s", out)
	}
	c := got.Calls[0]
	if c.Caller != "***991" || c.Outcome != "moved" || c.Turns != 1 || c.DurationS != 131 || got.Calls[1].Outcome != "hung up" {
		t.Fatalf("row = %+v / %+v", c, got.Calls[1])
	}
}

func TestRunCallShortIDAndSteps(t *testing.T) {
	srv := phoneCallsServer(t)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "run", "call", "#FD93D291", "--project", "dentai-phone-nl", "--tenant", "demo", "--steps"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"caller ***991 · 2:11 · dentai-phone-nl/demo", "Caller  Verplaatsen.", "⚙ move --yes → moved", "Outcome: moved maandag 30 november om 14 uur"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	e, _, _ = newTestEnv(t, srv, "table")
	if err := run(t, e, "run", "call", "fd93", "--project", "dentai-phone-nl", "--tenant", "demo"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("a shared prefix must refuse, got %v", err)
	}
}

func TestRunCallsScopeProjectDropsTenant(t *testing.T) {
	var tenants []string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"id":"a","name":"dentai-phone-nl"}]}`))
	})
	mux.HandleFunc("GET /api/v1/projects/dentai-phone-nl/phone-lab/runs", func(w http.ResponseWriter, r *http.Request) {
		tenants = append(tenants, r.URL.Query().Get("tenant"))
		_, _ = w.Write([]byte(`{"items":[],"next_cursor":""}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "run", "calls", "--project", "dentai-phone-nl", "--tenant", "demo", "--scope", "project"); err != nil {
		t.Fatal(err)
	}
	if len(tenants) != 1 || tenants[0] != "" || !strings.Contains(out.String(), "No calls.") {
		t.Fatalf("--scope project must list every tenant: %v %s", tenants, out)
	}
}

func TestParseCallsSince(t *testing.T) {
	now := time.Date(2026, 11, 26, 14, 0, 0, 0, time.UTC)
	for in, want := range map[string]time.Time{
		"90m":                  now.Add(-90 * time.Minute),
		"7d":                   now.AddDate(0, 0, -7),
		"2026-11-26":           time.Date(2026, 11, 26, 0, 0, 0, 0, time.UTC),
		"2026-11-26T12:00:00Z": time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC),
	} {
		got, err := parseCallsSince(in, now, time.UTC)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseCallsSince(%q) = %s, %v; want %s", in, got, err, want)
		}
	}
	if _, err := parseCallsSince("yesterday", now, time.UTC); err == nil {
		t.Error("free text must be rejected")
	}
}

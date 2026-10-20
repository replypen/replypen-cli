package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/config"
)

func TestPhoneLabDebug(t *testing.T) {
	const id = "7eab3bd3-d942-41b8-8e45-0b36086a31d9"
	const body = `{"project":"dentai-phone","run_id":"7eab3bd3-d942-41b8-8e45-0b36086a31d9","tenant":"practice","status":"closed","created_at":"2026-10-20T04:00:00Z","ended_at":"2026-10-20T04:00:10Z","caller":{"status":"lookup_failed","reason":"unavailable"},"future_field":{"kept":true},"events":[{"t":0,"kind":"transcript","who":"caller","text":"bonjour"},{"t":7000,"kind":"transcript","who":"agent","text":"oui","latency_ms":7000},{"t":8000,"kind":"tool","name":"bash","cmd":"false","exit_code":1,"stderr":"failed"},{"t":8100,"kind":"tool","status":"done","outcome":"error"},{"t":8200,"kind":"tool","status":"started"},{"t":8500,"kind":"commentary","status":"stale"},{"t":9000,"kind":"voicemail","status":"done","outcome":"created","future_audio":"kept"}]}`
	hits := 0
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"id":"a","name":"dentai-phone"}]}`))
	})
	mux.HandleFunc("GET /api/v1/projects/dentai-phone/phone-lab/runs/"+id, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("missing normal bearer")
		}
		if r.URL.Query().Get("tenant") != "" {
			t.Error("ambient brain tenant leaked into URL request")
		}
		hits++
		_, _ = w.Write([]byte(body))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	for _, bare := range []bool{false, true} {
		e, out, _ := newTestEnv(t, srv, "json")
		source := e.tokenSource
		e.tokenSource = func(res *config.Resolved, baseURL string) (client.TokenSource, error) {
			res.Tenant = "unrelated-ambient-tenant"
			return source(res, baseURL)
		}

		dir := t.TempDir()
		args := []string{"run", "debug", srv.URL + "/projects/dentai-phone/phone-lab/runs/" + id, "--out-dir", dir}
		if bare {
			args = []string{"run", "debug", id, "--phone-lab", "--project", "dentai-phone", "--out-dir", dir}
		}
		if err := run(t, e, args...); err != nil {
			t.Fatal(err)
		}
		var manifest map[string]any
		if err := json.Unmarshal(out.Bytes(), &manifest); err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "phone-"+id+".json"))
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) != body {
			t.Fatalf("raw JSON changed: %s", raw)
		}
		index, err := os.ReadFile(filepath.Join(dir, "phone-"+id+".md"))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"turn transcript latency 7000 ms", "bash exit 1", "commentary stale", "voicemail done created", "turn 7000 ms", "tool done error", "tool started", "Caller lookup failed"} {
			if !strings.Contains(string(index), want) {
				t.Errorf("index missing %q: %s", want, index)
			}
		}
		info, _ := os.Stat(filepath.Join(dir, "phone-"+id+".json"))
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", info.Mode())
		}
	}
	if hits != 2 {
		t.Fatalf("fetches=%d", hits)
	}
	for _, suffix := range [][]string{{"--project", "other"}, {"--share-token", "secret"}} {
		e, _, _ := newTestEnv(t, srv, "json")
		args := append([]string{"run", "debug", srv.URL + "/projects/dentai-phone/phone-lab/runs/" + id}, suffix...)
		if err := run(t, e, args...); err == nil {
			t.Fatal("expected conflict")
		}
	}
	e, _, _ := newTestEnv(t, srv, "json")
	if err := run(t, e, "run", "debug", "https://evil.example/projects/dentai-phone/phone-lab/runs/"+id); err == nil || !strings.Contains(err.Error(), "origin") {
		t.Fatalf("origin rejection=%v", err)
	}
	if hits != 2 {
		t.Fatal("rejected requests fetched run")
	}
}

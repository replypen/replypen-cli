package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnowledgeBundleCheck(t *testing.T) {
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bundle.json" {
			_, _ = io.WriteString(w, `{"schema":"broken"}`)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/kb-bundle/check" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected request %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		b, _ := io.ReadAll(r.Body)
		posted = string(b)
		if strings.Contains(posted, "broken") {
			_, _ = io.WriteString(w, `{"ok":false,"articles":0,"problems":[{"level":"error","message":"schema \"broken\""}]}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"articles":2,"problems":[{"level":"warning","article":"a","message":"link matches no article"}]}`)
	}))
	defer srv.Close()

	file := filepath.Join(t.TempDir(), "b.json")
	if err := os.WriteFile(file, []byte(`{"schema":"replypen.kb-bundle/1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "project", "knowledge", "bundle", "check", "--file", file); err != nil {
		t.Fatalf("ok bundle: %v", err)
	}
	if !strings.Contains(out.String(), "ok · 2 articles") || !strings.Contains(out.String(), "a: link matches no article") || posted != `{"schema":"replypen.kb-bundle/1"}` {
		t.Fatalf("output %q posted %q", out.String(), posted)
	}

	e, out, _ = newTestEnv(t, srv, "table")
	if err := run(t, e, "project", "knowledge", "bundle", "check", "--url", srv.URL+"/bundle.json"); err == nil {
		t.Fatal("a refused bundle must exit non-zero")
	}
	if !strings.Contains(out.String(), "REFUSED") {
		t.Fatalf("output %q", out.String())
	}

	e, out, _ = newTestEnv(t, srv, "json")
	if err := run(t, e, "project", "knowledge", "bundle", "check", "--url", srv.URL+"/bundle.json"); err == nil {
		t.Fatal("a refused bundle must exit non-zero in json mode too")
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil || doc["ok"] != false {
		t.Fatalf("json mode must print exactly one verdict document: %q (%v)", out.String(), err)
	}
}

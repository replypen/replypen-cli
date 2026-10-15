package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rootcause-org/rootcause-cli/internal/render"
)

// The generated script is run by a real POSIX sh: a quote+space path must arrive as one literal, and a
// dangling symlink must read as missing (the same answer a reading run would get).
func TestPathsScriptRunsUnderSh(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	quoted := filepath.Join(dir, "it's a $(file)")
	if err := os.WriteFile(quoted, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "gone"), dangling); err != nil {
		t.Fatal(err)
	}
	paths := []string{dir, quoted, dangling, filepath.Join(dir, "nope")}
	if got, want := pathsScriptLine(quoted), "c '"+dir+"/it'\\''s a $(file)'\n"; got != want {
		t.Fatalf("script line = %q, want %q", got, want)
	}
	out, err := exec.Command(sh, "-c", pathsScript(paths)).Output()
	if err != nil {
		t.Fatalf("sh: %v", err)
	}
	entries, err := parsePathsOutput(paths, string(out))
	if err != nil {
		t.Fatalf("parse %q: %v", out, err)
	}
	var kinds []string
	for _, e := range entries {
		kinds = append(kinds, e.Kind)
	}
	if got := strings.Join(kinds, ","); got != "dir,file,missing,missing" {
		t.Fatalf("kinds = %s", got)
	}
}

func TestParsePathsOutputIsStrict(t *testing.T) {
	chunk := []string{"/a", "/b"}
	for name, stdout := range map[string]string{
		"missing line":   "file\t/a\n",
		"unknown kind":   "file\t/a\nlink\t/b\n",
		"foreign path":   "file\t/a\nfile\t/b\nfile\t/c\n",
		"duplicate path": "file\t/a\nfile\t/a\nfile\t/b\n",
		"noise":          "welcome\nfile\t/a\nfile\t/b\n",
		"empty":          "",
	} {
		if _, err := parsePathsOutput(chunk, stdout); err == nil {
			t.Errorf("%s: parse accepted %q", name, stdout)
		}
	}
	entries, err := parsePathsOutput(chunk, "missing\t/b\ndir\t/a\n")
	if err != nil || entries[0] != (render.PathEntry{Path: "/a", Kind: "dir"}) || entries[1] != (render.PathEntry{Path: "/b", Kind: "missing"}) {
		t.Fatalf("entries = %+v, %v", entries, err)
	}
}

func TestChunkPathsStaysWithinBudget(t *testing.T) {
	var paths []string
	for i := range 3000 {
		paths = append(paths, fmt.Sprintf("/brain/skills/some/long/directory/name/%04d/it's.md", i))
	}
	chunks := chunkPaths(paths)
	if len(chunks) < 2 {
		t.Fatalf("chunks = %d, want several", len(chunks))
	}
	var joined []string
	for _, c := range chunks {
		if n := len(pathsScript(c)); n > pathsChunkBudget {
			t.Fatalf("chunk script %d bytes > budget %d", n, pathsChunkBudget)
		}
		joined = append(joined, c...)
	}
	if strings.Join(joined, "\n") != strings.Join(paths, "\n") {
		t.Fatal("chunks lost or reordered paths")
	}
}

// pathsStub answers console bash runs by decoding the script's literals and looking them up in the
// per-tenant filesystem; tenant "broken" exits non-zero. It records the calls per tenant.
func pathsStub(t *testing.T, fs map[string]map[string]string) (*httptest.Server, func() map[string]int) {
	t.Helper()
	var mu sync.Mutex
	calls := map[string]int{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/projects", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"projects":[{"id":"aaaaaaaa-0000-0000-0000-000000000001","name":"alpha"}]}`))
	})
	mux.HandleFunc("GET /api/v1/projects/alpha/tenants", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"id":"acme","slug":"acme","status":"active"},{"id":"old","slug":"old","status":"archived"},{"id":"broken","slug":"broken","status":"active"},{"id":"beta","slug":"beta","status":"active"}]`))
	})
	mux.HandleFunc("POST /api/v1/console/bash/run", func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		var req struct{ Command string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		tenant := r.URL.Query().Get("tenant")
		mu.Lock()
		calls[tenant]++
		n := calls[tenant]
		mu.Unlock()
		if tenant == "flaky" && n == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		resp := map[string]any{"project": "alpha", "tenant": tenant, "brain_resolved": "channel:stable @ abc123", "run_id": "run-" + tenant, "seq": 1}
		if tenant == "broken" {
			resp["exit_code"], resp["stderr"] = 2, "sh: boom"
			_ = json.NewEncoder(w).Encode(resp)
			return
		}
		var out strings.Builder
		for _, line := range strings.Split(req.Command, "\n") {
			lit, ok := strings.CutPrefix(line, "c '")
			if !ok {
				continue
			}
			p := strings.ReplaceAll(strings.TrimSuffix(lit, "'"), `'\''`, "'")
			kind := fs[tenant][p]
			if kind == "" {
				kind = "missing"
			}
			out.WriteString(kind + "\t" + p + "\n")
		}
		resp["stdout"] = out.String()
		_ = json.NewEncoder(w).Encode(resp)
	})
	srv := httptest.NewServer(mux)
	return srv, func() map[string]int { mu.Lock(); defer mu.Unlock(); return calls }
}

func TestConsolePathsFanOutAllTenants(t *testing.T) {
	srv, calls := pathsStub(t, map[string]map[string]string{
		"acme": {"/brain/AGENTS.md": "file", "/tenant/it's here": "dir"},
		"beta": {"/brain/AGENTS.md": "file"},
	})
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "json")
	err := run(t, e, "--project", "alpha", "dev", "console", "paths", "--tenants", "all", "--concurrency", "2",
		"/brain/AGENTS.md", "/tenant/it's here", "/brain/AGENTS.md")
	if exitCodeFor(err) != exitRemote {
		t.Fatalf("exit = %d (%v), want %d", exitCodeFor(err), err, exitRemote)
	}
	var report render.PathsReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("decode %s: %v", out.String(), err)
	}
	if report.Project != "alpha" || strings.Join(report.Paths, "|") != "/brain/AGENTS.md|/tenant/it's here" {
		t.Fatalf("report header = %+v", report)
	}
	byTenant := map[string]render.PathsResult{}
	var order []string
	for _, r := range report.Results {
		byTenant[r.Tenant] = r
		order = append(order, r.Tenant)
	}
	if strings.Join(order, ",") != "acme,broken,beta" {
		t.Fatalf("tenants = %v (archived must be skipped, order kept)", order)
	}
	if got := byTenant["acme"].Entries; len(got) != 2 || got[1].Kind != "dir" {
		t.Fatalf("acme entries = %+v", got)
	}
	if got := byTenant["beta"]; got.Entries[1].Kind != "missing" || got.BrainResolved != "channel:stable @ abc123" || got.RunIDs[0] != "run-beta" {
		t.Fatalf("beta = %+v", got)
	}
	if got := byTenant["broken"]; got.Entries != nil || !strings.Contains(got.Error, "exited 2: sh: boom") {
		t.Fatalf("broken = %+v", got)
	}
	if c := calls(); c["old"] != 0 || c["acme"] != 1 {
		t.Fatalf("calls = %v", c)
	}
}

// Stdin input is chunked into several calls for one scope, and an all-present answer exits 0.
func TestConsolePathsStdinChunksOneScope(t *testing.T) {
	fs := map[string]string{}
	var lines []string
	for i := range 2000 {
		p := fmt.Sprintf("/mirrors/app/docs/some/fairly/long/path/segment/%04d.md", i)
		fs[p] = "file"
		lines = append(lines, p, "  ")
	}
	srv, calls := pathsStub(t, map[string]map[string]string{"acme": fs})
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "json")
	e.in = strings.NewReader(strings.Join(lines, "\n"))
	if err := run(t, e, "--raw-output", "--project", "alpha", "--tenant", "acme", "dev", "console", "paths", "-"); err != nil {
		t.Fatalf("paths: %v", err)
	}
	if n := calls()["acme"]; n < 2 {
		t.Fatalf("calls = %d, want chunked", n)
	}
	var report render.PathsReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if len(report.Results) != 1 || len(report.Results[0].Entries) != 2000 || len(report.Results[0].RunIDs) != 1 {
		t.Fatalf("results = %d entries=%d", len(report.Results), len(report.Results[0].Entries))
	}
}

// A gateway error (server recreate) is re-sent: the path script only reads.
func TestConsolePathsRetriesGatewayError(t *testing.T) {
	saved := pathsRetryDelays
	pathsRetryDelays = []time.Duration{0}
	defer func() { pathsRetryDelays = saved }()
	srv, calls := pathsStub(t, map[string]map[string]string{"flaky": {"/kb/x.md": "file"}})
	defer srv.Close()
	e, _, _ := newTestEnv(t, srv, "json")
	if err := run(t, e, "--project", "alpha", "--tenant", "flaky", "dev", "console", "paths", "/kb/x.md"); err != nil {
		t.Fatalf("paths: %v", err)
	}
	if n := calls()["flaky"]; n != 2 {
		t.Fatalf("calls = %d, want 2", n)
	}
}

package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func truncatedBashServer(t *testing.T, full string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/console/bash/run", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"project": "kampadmin", "tenant": "dev_org", "run_id": "11111111-aaaa", "seq": 7,
			"stdout": full[:10] + "\n…[truncated]", "stdout_truncated": true,
			"stdout_path": "/tmp/rc-console/stdout-7.txt", "stdout_bytes": len(full),
		})
	})
	mux.HandleFunc("GET /api/v1/console/file", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") != "/tmp/rc-console/stdout-7.txt" {
			t.Errorf("file get query = %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(full))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestBashRunTruncatedPrintsDownloadHint(t *testing.T) {
	full := strings.Repeat("x", 70000)
	want := "rc dev console file get /tmp/rc-console/stdout-7.txt ./stdout-7.txt --tenant dev_org"
	for _, output := range []string{"table", "json"} {
		t.Run(output, func(t *testing.T) {
			e, out, _ := newTestEnv(t, truncatedBashServer(t, full), output)
			if err := run(t, e, "dev", "console", "bash", "run", "cat big"); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), want) {
				t.Fatalf("output lacks the download command:\n%s", out.String())
			}
		})
	}
}

func TestBashRunOutFetchesFullTruncatedStdout(t *testing.T) {
	full := strings.Repeat("y", 70000)
	e, _, errb := newTestEnv(t, truncatedBashServer(t, full), "table")
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := run(t, e, "dev", "console", "bash", "run", "--out", path, "cat big"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != full {
		t.Fatalf("--out wrote %d bytes, want the full %d", len(b), len(full))
	}
	if strings.Contains(errb.String(), "truncated") {
		t.Fatalf("full fetch still warned: %s", errb.String())
	}
}

func TestFileGetDirectoryStagesAndUnpacks(t *testing.T) {
	var tarball bytes.Buffer
	gz := gzip.NewWriter(&tarball)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ name, body string }{{"./a.md", "alpha"}, {"./sub/b.md", "beta"}} {
		_ = tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o644, Size: int64(len(f.body)), Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte(f.body))
	}
	_ = tw.WriteHeader(&tar.Header{Name: "./escape", Linkname: "../../etc/passwd", Typeflag: tar.TypeSymlink})
	_ = tw.Close()
	_ = gz.Close()

	var commands []string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/console/bash/run", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Command string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		commands = append(commands, req.Command)
		stdout := ""
		if strings.HasPrefix(req.Command, "p=/kb/knowledgeowl") {
			stdout = "dir\t/tmp/rc-download-abc\tstaged\n"
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"run_id": "r", "stdout": stdout})
	})
	mux.HandleFunc("GET /api/v1/console/file", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") != "/tmp/rc-download-abc" {
			t.Errorf("fetched %q", r.URL.Query().Get("path"))
		}
		_, _ = w.Write(tarball.Bytes())
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "table")
	dst := filepath.Join(t.TempDir(), "kb")
	if err := run(t, e, "dev", "console", "file", "get", "/kb/knowledgeowl", dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "sub", "b.md")); string(b) != "beta" {
		t.Fatalf("sub/b.md = %q", b)
	}
	if _, err := os.Lstat(filepath.Join(dst, "escape")); !os.IsNotExist(err) {
		t.Fatalf("escaping symlink was created (err=%v)", err)
	}
	if len(commands) != 2 || commands[1] != "rm -f -- /tmp/rc-download-abc" {
		t.Fatalf("bash commands = %q, want stage + cleanup", commands)
	}
	if !strings.Contains(out.String(), "2 files") {
		t.Fatalf("summary = %s", out.String())
	}
}

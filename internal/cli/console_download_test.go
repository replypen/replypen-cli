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
	t.Run("table", func(t *testing.T) {
		e, out, _ := newTestEnv(t, truncatedBashServer(t, full), "table")
		if err := run(t, e, "dev", "console", "bash", "run", "cat big"); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output lacks the download command:\n%s", out.String())
		}
	})
	// JSON stdout is the verbatim server body: the CLI hint goes to stderr, never into the body.
	t.Run("json", func(t *testing.T) {
		e, out, errb := newTestEnv(t, truncatedBashServer(t, full), "json")
		if err := run(t, e, "dev", "console", "bash", "run", "cat big"); err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.Unmarshal(out.Bytes(), &body); err != nil {
			t.Fatalf("stdout is not JSON: %v (%s)", err, out.String())
		}
		if _, ok := body["hint"]; ok {
			t.Fatalf("hint spliced into the server body: %s", out.String())
		}
		if !strings.Contains(errb.String(), want) {
			t.Fatalf("stderr lacks the download command: %q", errb.String())
		}
	})
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
	// Chained links: a -> ., b -> a/.. (outside), then b/escaped would land in the parent if links were made.
	_ = tw.WriteHeader(&tar.Header{Name: "./a", Linkname: ".", Typeflag: tar.TypeSymlink})
	_ = tw.WriteHeader(&tar.Header{Name: "./b", Linkname: "a/..", Typeflag: tar.TypeSymlink})
	_ = tw.WriteHeader(&tar.Header{Name: "./b/escaped", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("!"))
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
	if _, err := os.Lstat(filepath.Join(dst, "a")); !os.IsNotExist(err) {
		t.Fatalf("symlink was created (err=%v)", err)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(dst), "escaped")); !os.IsNotExist(err) {
		t.Fatalf("chained symlink escaped the destination (err=%v)", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "b", "escaped")); string(b) != "!" {
		t.Fatalf("b/escaped = %q, want a plain file inside the tree", b)
	}
	if len(commands) != 2 || commands[1] != "rm -f -- /tmp/rc-download-abc" {
		t.Fatalf("bash commands = %q, want stage + cleanup", commands)
	}
	if !strings.Contains(out.String(), "3 files") {
		t.Fatalf("summary = %s", out.String())
	}
}

// TestExtractTarGzRejectsCorruptTrailer: the gzip CRC sits after tar's end-of-archive blocks, so the
// extractor must read through it — a corrupt download may not be installed as a complete tree.
func TestExtractTarGzRejectsCorruptTrailer(t *testing.T) {
	var tarball bytes.Buffer
	gz := gzip.NewWriter(&tarball)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "a.md", Mode: 0o644, Size: 5, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("alpha"))
	_ = tw.Close()
	_ = gz.Close()
	if _, err := extractTarGz(bytes.NewReader(tarball.Bytes()), t.TempDir()); err != nil {
		t.Fatalf("valid archive: %v", err)
	}
	corrupt := bytes.Clone(tarball.Bytes())
	corrupt[len(corrupt)-8] ^= 0xff // first CRC-32 byte of the gzip trailer
	if _, err := extractTarGz(bytes.NewReader(corrupt), t.TempDir()); err == nil {
		t.Fatal("corrupt gzip trailer extracted without error")
	}
}

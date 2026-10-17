package token

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixedNow() time.Time { return time.Date(2026, 10, 17, 6, 0, 0, 0, time.UTC) }

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The archived pre-rename rp owns ~/.config/replypen (config.toml, other schema): move it aside, then
// adopt the rootcause store by rename + symlink.
func TestMigrateMovesForeignReplypenDirAside(t *testing.T) {
	base := t.TempDir()
	writeFile(t, filepath.Join(base, "replypen", "config.toml"), "token = \"old-rp\"\n")
	writeFile(t, filepath.Join(base, "rootcause", "tokens.json"), `{"profiles":{}}`)

	var notices bytes.Buffer
	dir := migrateConfigDir(base, &notices, fixedNow)

	if dir != filepath.Join(base, "replypen") {
		t.Fatalf("dir = %s", dir)
	}
	aside := filepath.Join(base, "replypen.rp-legacy-20261017T060000Z")
	if got := readFile(t, filepath.Join(aside, "config.toml")); !strings.Contains(got, "old-rp") {
		t.Fatalf("aside config = %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "tokens.json")); got != `{"profiles":{}}` {
		t.Fatalf("tokens not moved: %q", got)
	}
	if !strings.Contains(notices.String(), aside) {
		t.Fatalf("notice %q does not name %s", notices.String(), aside)
	}
}

// Only the old dir: rename it (never copy) and leave rootcause → replypen, so old rc builds and rp
// share one tokens.json and one lock namespace. A second run is a no-op.
func TestMigrateRenamesRootcauseAndLeavesSymlink(t *testing.T) {
	base := t.TempDir()
	writeFile(t, filepath.Join(base, "rootcause", "tokens.json"), `{"profiles":{"default":{}}}`)

	var notices bytes.Buffer
	dir := migrateConfigDir(base, &notices, fixedNow)
	if dir != filepath.Join(base, "replypen") {
		t.Fatalf("dir = %s", dir)
	}
	old := filepath.Join(base, "rootcause")
	if target, err := os.Readlink(old); err != nil || target != "replypen" {
		t.Fatalf("rootcause symlink = %q, %v", target, err)
	}
	// Writing through the old path lands in the one shared store.
	writeFile(t, filepath.Join(old, "tokens.json"), "via-old")
	if got := readFile(t, filepath.Join(dir, "tokens.json")); got != "via-old" {
		t.Fatalf("store not shared: %q", got)
	}
	if notices.Len() != 0 {
		t.Fatalf("unexpected notice: %q", notices.String())
	}

	if again := migrateConfigDir(base, &notices, fixedNow); again != dir || notices.Len() != 0 {
		t.Fatalf("second run: dir %s, notices %q", again, notices.String())
	}
}

// Both real dirs: replypen wins and the warning is shown once, not on every invocation.
func TestMigrateBothRealDirsWarnsOnce(t *testing.T) {
	base := t.TempDir()
	writeFile(t, filepath.Join(base, "replypen", "tokens.json"), "new")
	writeFile(t, filepath.Join(base, "rootcause", "tokens.json"), "old")

	var first bytes.Buffer
	if dir := migrateConfigDir(base, &first, fixedNow); dir != filepath.Join(base, "replypen") {
		t.Fatalf("dir = %s", dir)
	}
	if !strings.Contains(first.String(), "both") {
		t.Fatalf("missing warning: %q", first.String())
	}
	var second bytes.Buffer
	migrateConfigDir(base, &second, fixedNow)
	if second.Len() != 0 {
		t.Fatalf("warned twice: %q", second.String())
	}
	if got := readFile(t, filepath.Join(base, "rootcause", "tokens.json")); got != "old" {
		t.Fatalf("old dir touched: %q", got)
	}
}

// End to end through the public API under a temp HOME-style XDG base.
func TestStoreReadsMigratedLegacyTokens(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	Notices = &bytes.Buffer{}
	t.Cleanup(func() { Notices = os.Stderr })
	writeFile(t, filepath.Join(base, "rootcause", "tokens.json"), `{"profiles":{"default":{"access_token":"at"}}}`)

	tok, ok, err := Load("default")
	if err != nil || !ok || tok.AccessToken != "at" {
		t.Fatalf("Load = %+v, %v, %v", tok, ok, err)
	}
	path, _ := Path()
	if path != filepath.Join(base, "replypen", "tokens.json") {
		t.Fatalf("Path = %s", path)
	}
}

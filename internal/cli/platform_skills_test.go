package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func tarGz(t *testing.T, files map[string]string, modes map[string]int64) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		mode := modes[name]
		if mode == 0 {
			mode = 0o644
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(body))
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestExtractPlatformSkillsNamesTheDirByItsContentAndKeepsExecBit(t *testing.T) {
	root := t.TempDir()
	archive := tarGz(t, map[string]string{"website/SKILL.md": "x", "website/scripts/sitegen.py": "X = 1\n"},
		map[string]int64{"website/scripts/sitegen.py": 0o755})
	digest, err := extractPlatformSkills(archive, root, "")
	if err != nil {
		t.Fatal(err)
	}
	again, err := extractPlatformSkills(archive, root, digest)
	if err != nil || again != digest {
		t.Fatalf("re-extract = %q, %v; want %q", again, err, digest)
	}
	info, err := os.Stat(filepath.Join(root, digest, "website", "scripts", "sitegen.py"))
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("sitegen.py = %v, %v", info, err)
	}
	if got := skillNames(filepath.Join(root, digest)); len(got) != 1 || got[0] != "website" {
		t.Fatalf("skills = %v", got)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 1 {
		t.Fatalf("temp dirs left behind: %v", entries)
	}
}

func TestExtractPlatformSkillsRejectsUnsafePaths(t *testing.T) {
	for _, name := range []string{"../evil.py", "/etc/evil"} {
		if _, err := extractPlatformSkills(tarGz(t, map[string]string{name: "x"}, nil), t.TempDir(), ""); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
}

func TestOfflineWithoutCacheSaysHowToPull(t *testing.T) {
	if _, err := cachedPlatformSkills(t.TempDir()); err == nil {
		t.Fatal("empty cache returned a snapshot")
	}
}

func TestExtractPlatformSkillsIsReadableByOthersUnderATightUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	root := t.TempDir()
	digest, err := extractPlatformSkills(tarGz(t, map[string]string{"pdf/scripts/x.py": "x"}, nil), root, "")
	if err != nil {
		t.Fatal(err)
	}
	for rel, want := range map[string]os.FileMode{"": 0o755, "pdf": 0o755, "pdf/scripts": 0o755, "pdf/scripts/x.py": 0o644} {
		info, err := os.Stat(filepath.Join(root, digest, rel))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != want {
			t.Fatalf("%q mode %v, want %v", rel, info.Mode().Perm(), want)
		}
	}
}

func TestCurrentPointerIsAtomicAndValidated(t *testing.T) {
	root := t.TempDir()
	digest := strings.Repeat("a", 64)
	if err := os.MkdirAll(filepath.Join(root, digest, "website"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "current"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := cachedPlatformSkills(root); err == nil {
		t.Fatal("empty pointer accepted")
	}
	if err := writeCurrent(root, digest); err != nil {
		t.Fatal(err)
	}
	got, err := cachedPlatformSkills(root)
	if err != nil || got.Digest != digest || len(got.Skills) != 1 || got.Skills[0] != "website" {
		t.Fatalf("cached = %+v, %v", got, err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 2 {
		t.Fatalf("temp pointer left behind: %v", entries)
	}
}

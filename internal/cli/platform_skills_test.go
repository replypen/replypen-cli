package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
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

package localstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSeedIgnore(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{Dir, LegacyDir} {
		target := filepath.Join(root, dir, "debug", "x")
		if err := SeedIgnore(target); err != nil {
			t.Fatal(err)
		}
		if body, err := os.ReadFile(filepath.Join(root, dir, ".gitignore")); err != nil || string(body) != "*\n" {
			t.Fatalf("%s: .gitignore = %q, %v", dir, body, err)
		}
	}

	outside := filepath.Join(root, "exports")
	if err := SeedIgnore(outside); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(outside, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf("a dir outside local state must be left alone: %v", err)
	}
}

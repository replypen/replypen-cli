// Package localstate names the per-checkout artifact dir every rp write lands in (run dumps, spills,
// exports, attachments, imap env files) and keeps it uncommittable. The dir was `.rootcause/` before the
// ReplyPen rename; brains only gitignore that legacy name, so every write seeds its own `*` .gitignore.
package localstate

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	Dir       = ".replypen"
	LegacyDir = ".rootcause"
)

// Path joins elem under the local state dir (relative to the cwd).
func Path(elem ...string) string {
	return filepath.Join(append([]string{Dir}, elem...)...)
}

// SeedIgnore makes sure a path under a .replypen (or legacy .rootcause) dir can never be committed: it
// writes a `*` .gitignore at that dir's root, once, leaving an existing one alone. Paths outside such a
// dir are the caller's to gitignore, so this is a no-op for them.
func SeedIgnore(path string) error {
	root := stateRoot(path)
	if root == "" {
		return nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", root, err)
	}
	gi := filepath.Join(root, ".gitignore")
	if _, err := os.Stat(gi); err == nil {
		return nil
	}
	if err := os.WriteFile(gi, []byte("*\n"), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", gi, err)
	}
	return nil
}

// stateRoot returns the outermost .replypen/.rootcause dir path sits in, or "".
func stateRoot(path string) string {
	found := ""
	for p := filepath.Clean(path); ; {
		if base := filepath.Base(p); base == Dir || base == LegacyDir {
			found = p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return found
		}
		p = parent
	}
}

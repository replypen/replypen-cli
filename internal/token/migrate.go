package token

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The store moved from <config>/rootcause to <config>/replypen with the ReplyPen rename. The old
// directory is renamed (never copied) and replaced by a symlink, so old rc and new rp binaries share ONE
// tokens.json and one lock namespace: a copy would let both rotate the same single-use refresh token.
const (
	dirName       = "replypen"
	legacyDirName = "rootcause"
	// Written into the new dir once the both-dirs warning has been shown, so it is shown once.
	legacyWarnedMarker = ".legacy-rootcause-dir-warned"
)

// Notices receives the one-line migration notices (stderr; tests swap it).
var Notices io.Writer = os.Stderr

var (
	migrateMu sync.Mutex
	migrated  = map[string]string{} // config base → resolved store dir, once per process
)

// Migrate runs the idempotent config-dir migration now. The CLI calls it at startup; every store
// access also goes through it, so nothing can read tokens from a half-migrated layout.
func Migrate() {
	_, _ = configDir()
}

func configBase() (string, error) {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return xdg, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locate home dir: %w", err)
	}
	return filepath.Join(home, ".config"), nil
}

// configDir is the resolved ~/.config/replypen directory (XDG-style; honors XDG_CONFIG_HOME).
func configDir() (string, error) {
	base, err := configBase()
	if err != nil {
		return "", err
	}
	migrateMu.Lock()
	defer migrateMu.Unlock()
	if dir, ok := migrated[base]; ok {
		return dir, nil
	}
	dir := migrateConfigDir(base, Notices, time.Now)
	migrated[base] = dir
	return dir, nil
}

type dirState int

const (
	dirAbsent dirState = iota
	dirSymlink
	dirReal
)

func stateOf(path string) dirState {
	info, err := os.Lstat(path)
	switch {
	case err != nil:
		return dirAbsent
	case info.Mode()&os.ModeSymlink != 0:
		return dirSymlink
	default:
		return dirReal
	}
}

// isStoreDir: empty, or holding our tokens.json / its lock files. Anything else in <config>/replypen
// belongs to the archived pre-rename `rp` CLI (a config.toml with a different schema).
func isStoreDir(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	if len(entries) == 0 {
		return true
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), fileName) {
			return true
		}
	}
	return false
}

// migrateConfigDir returns the store dir to use under base, migrating the layout first:
//
//	foreign <base>/replypen          → moved aside to replypen.rp-legacy-<UTC ts>, with a notice
//	only <base>/rootcause (real dir) → renamed to replypen; rootcause becomes a symlink to it
//	both real                        → replypen wins; warn once
//
// A failed rename falls back to the old dir rather than stranding the user's tokens.
func migrateConfigDir(base string, w io.Writer, now func() time.Time) string {
	cur := filepath.Join(base, dirName)
	old := filepath.Join(base, legacyDirName)
	curState, oldState := stateOf(cur), stateOf(old)

	if curState == dirReal && !isStoreDir(cur) {
		aside := cur + ".rp-legacy-" + now().UTC().Format("20060102T150405Z")
		if err := os.Rename(cur, aside); err != nil {
			_, _ = fmt.Fprintf(w, "warning: %s is not a ReplyPen CLI config dir and could not be moved aside: %v\n", cur, err)
			if oldState != dirAbsent {
				return old
			}
			return cur
		}
		_, _ = fmt.Fprintf(w, "notice: moved the archived rp CLI's config %s aside to %s\n", cur, aside)
		curState = dirAbsent
	}

	switch {
	case curState == dirAbsent && oldState == dirReal:
		// Without a symlink older rc builds would lose the shared store (e.g. Windows without symlink
		// rights): keep using the legacy dir there rather than split the tokens.
		if !canSymlink(base) {
			return old
		}
		if err := os.Rename(old, cur); err != nil {
			if stateOf(cur) == dirReal { // a concurrent rp process migrated first
				return cur
			}
			_, _ = fmt.Fprintf(w, "warning: could not move %s to %s (%v); using the old location\n", old, cur, err)
			return old
		}
		if err := os.Symlink(dirName, old); err != nil && stateOf(old) != dirSymlink {
			if rbErr := os.Rename(cur, old); rbErr == nil { // never leave old rc builds without the store
				return old
			}
			_, _ = fmt.Fprintf(w, "warning: moved %s to %s, but could not leave a symlink for older rc builds: %v\n", old, cur, err)
		}
	case curState == dirReal && oldState == dirReal:
		marker := filepath.Join(cur, legacyWarnedMarker)
		if _, err := os.Stat(marker); err != nil {
			_, _ = fmt.Fprintf(w, "warning: both %s and %s exist; using %s. Older rc builds still read %s: once they are gone, remove it or replace it with a symlink to %s\n", cur, old, cur, old, dirName)
			_ = os.WriteFile(marker, nil, storeMode)
		}
	}
	return cur
}

func canSymlink(base string) bool {
	probe := filepath.Join(base, fmt.Sprintf(".replypen-symlink-probe-%d", os.Getpid()))
	_ = os.Remove(probe)
	if err := os.Symlink(dirName, probe); err != nil {
		return false
	}
	_ = os.Remove(probe)
	return true
}

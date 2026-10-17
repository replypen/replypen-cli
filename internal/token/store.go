// Package token is the on-disk OAuth credential store: ~/.config/replypen/tokens.json (0600), a
// per-profile record of {access_token, refresh_token, expiry, base_url}. The CLI is OAuth-only: it
// holds no static API key, only short-lived access tokens refreshed from a stored refresh token.
//
// The store is intentionally dumb: it persists and returns what it's given. WHO refreshes (and the
// pre-expiry/401 policy) lives one layer up, in the CLI's token source — this package only owns the
// file format, the 0600 mode, and the per-profile keying.
package token

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// fileName is the token store under the ReplyPen config dir (see migrate.go for the dir itself).
const fileName = "tokens.json"

// storeMode is owner-only: the file holds live OAuth refresh + access tokens.
const storeMode = 0o600

var storeMu sync.Mutex

// Two lock scopes, always taken in this order (profile, then store) and never the reverse:
//   - the STORE lock (tokens.json.lock) covers one read-modify-write of the file — milliseconds, so a
//     short wait suffices and a stuck holder surfaces quickly;
//   - a PROFILE lock (tokens.json.<profile>.lock) covers Update's whole check → refresh → persist cycle
//     for one profile. It is held across an OAuth round-trip, so its wait must exceed the OAuth client's
//     30s HTTP timeout: a waiter outlasts one sibling's in-flight refresh, then adopts its result.
//     Per-profile scoping keeps a slow refresh on one profile from queueing every other profile.
const (
	storeLockWait   = 5 * time.Second
	profileLockWait = 45 * time.Second
)

// Token is one profile's stored credential. ExpiresAt is the access token's absolute expiry (computed
// at mint/refresh time from the server's expires_in). RefreshToken is empty for a non-rotating machine
// credential that never returned one — but in practice the CLI always logs in with a rotating grant, so
// it's set. BaseURL records the issuer used at login/latest refresh for diagnostics and revocation; it
// does not override normal command URL resolution.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	BaseURL      string    `json:"base_url"`
	// MachineTokenEnv records provenance only (never the secret) so removing a declared cloud secret
	// disables its cached machine credential without breaking ordinary OAuth profiles.
	MachineTokenEnv string `json:"machine_token_env,omitempty"`
}

// Expired reports whether the access token is at/after its expiry, with a skew margin so a token about
// to expire is treated as already expired (refresh it before the request, not after a 401). A zero
// ExpiresAt (unknown) is treated as expired so the caller refreshes rather than sending a stale token.
func (t Token) Expired(now time.Time, skew time.Duration) bool {
	if t.ExpiresAt.IsZero() {
		return true
	}
	return !now.Add(skew).Before(t.ExpiresAt)
}

// storeFile is the JSON envelope: profile name → token.
type storeFile struct {
	Profiles map[string]Token `json:"profiles"`
}

// Path is the resolved tokens.json location (exported for diagnostics/messages).
func Path() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}

// Load returns the token stored for profile, with ok=false when there is none (no file, or no such
// profile). A malformed store is an error so a corrupted file surfaces loudly rather than silently
// looking "logged out".
func Load(profile string) (Token, bool, error) {
	sf, err := read()
	if err != nil {
		return Token{}, false, err
	}
	t, ok := sf.Profiles[profile]
	return t, ok, nil
}

// Save writes (or replaces) the token for profile, preserving every other profile's entry, at 0600. It
// waits out an in-flight Update on the same profile so a refresh cannot overwrite a fresh login.
func Save(profile string, t Token) error {
	unlock, err := lockProfile(profile)
	if err != nil {
		return err
	}
	defer unlock()
	return mutate(func(sf *storeFile) {
		if sf.Profiles == nil {
			sf.Profiles = map[string]Token{}
		}
		sf.Profiles[profile] = t
	})
}

// Update runs fn under profile's cross-process lock with its CURRENT token (ok=false if none) and
// persists next when save is true. The whole read → fn → write cycle is one critical section for that
// profile, so fn may do network I/O — the CLI's check-expiry → refresh → persist — without a sibling `rp`
// process presenting the same single-use refresh token. Other profiles are not blocked: only the final
// write takes the store-wide lock, briefly. fn must not call Save/Delete for the same profile (it would
// wait on the lock it is running under).
func Update(profile string, fn func(cur Token, ok bool) (next Token, save bool, err error)) error {
	unlock, err := lockProfile(profile)
	if err != nil {
		return err
	}
	defer unlock()
	// A lockless read is safe: writers replace the file by rename, and this profile's entry only changes
	// under the profile lock we hold.
	sf, err := read()
	if err != nil {
		return err
	}
	cur, ok := sf.Profiles[profile]
	next, save, err := fn(cur, ok)
	if err != nil || !save {
		return err
	}
	return mutate(func(sf *storeFile) { sf.Profiles[profile] = next })
}

// Delete removes the token for profile (a no-op if absent). Used by `rp auth logout`.
func Delete(profile string) error {
	unlock, err := lockProfile(profile)
	if err != nil {
		return err
	}
	defer unlock()
	return mutate(func(sf *storeFile) { delete(sf.Profiles, profile) })
}

// List returns every stored profile→token (a copy). Used by diagnostics/whoami.
func List() (map[string]Token, error) {
	sf, err := read()
	if err != nil {
		return nil, err
	}
	return sf.Profiles, nil
}

// read loads the store; a missing file is an empty store, not an error.
func read() (storeFile, error) {
	path, err := Path()
	if err != nil {
		return storeFile{}, err
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return storeFile{Profiles: map[string]Token{}}, nil
	}
	if err != nil {
		return storeFile{}, fmt.Errorf("read %s: %w", path, err)
	}
	var sf storeFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return storeFile{}, fmt.Errorf("parse %s: %w (delete it and `rp auth login` again)", path, err)
	}
	if sf.Profiles == nil {
		sf.Profiles = map[string]Token{}
	}
	return sf, nil
}

// mutate is the store-locked read-modify-write of tokens.json.
func mutate(change func(*storeFile)) error {
	storeMu.Lock()
	defer storeMu.Unlock()

	path, err := Path()
	if err != nil {
		return err
	}
	unlock, err := acquireLock(path+".lock", storeLockWait)
	if err != nil {
		return err
	}
	defer unlock()

	sf, err := read()
	if err != nil {
		return err
	}
	change(&sf)
	return write(sf)
}

// lockProfile takes profile's cross-process refresh lock (see profileLockWait).
func lockProfile(profile string) (func(), error) {
	path, err := Path()
	if err != nil {
		return nil, err
	}
	return acquireLock(path+"."+lockKey(profile)+".lock", profileLockWait)
}

// lockKey turns a profile name into a file-name-safe, collision-free key: a name of only [A-Za-z0-9_-]
// is used as-is, anything else is hex-encoded behind "=" (a character the as-is form never contains).
func lockKey(profile string) string {
	safe := func(r rune) bool {
		return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
	}
	if profile == "" || strings.IndexFunc(profile, func(r rune) bool { return !safe(r) }) >= 0 {
		return "=" + hex.EncodeToString([]byte(profile))
	}
	return profile
}

// acquireLock takes an exclusive advisory lock on lockPath, polling for up to wait. The lock file is
// intentionally persistent: the kernel releases the lock on close or process exit, so there is no
// stale-file cleanup race.
func acquireLock(lockPath string, wait time.Duration) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}
	f, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE, storeMode)
	if err != nil {
		return nil, fmt.Errorf("open token-store lock: %w", err)
	}
	deadline := time.Now().Add(wait)
	for {
		unlockFile, lockErr := tryLockFile(f)
		if lockErr == nil {
			return func() {
				_ = unlockFile()
				_ = f.Close()
			}, nil
		}
		if !lockContended(lockErr) {
			_ = f.Close()
			return nil, fmt.Errorf("lock token store: %w", lockErr)
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("token store is busy (lock %s)", lockPath)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// write persists the store atomically-ish (write a temp file, then rename) at 0600, creating the config
// dir if needed. The rename keeps a concurrent reader from ever seeing a half-written file.
func write(sf storeFile) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return fmt.Errorf("encode token store: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tokens-*.tmp")
	if err != nil {
		return fmt.Errorf("create token temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(storeMode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpPath, err)
	}
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

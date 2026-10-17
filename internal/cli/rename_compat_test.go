package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The rc → rp rename: `rc` stays a working command next to `rp`, and never reads as a second install.

func TestInspectRCInstallationsRcSymlinkIsSameInstall(t *testing.T) {
	root := t.TempDir()
	rp := executableFile(t, filepath.Join(root, "bin", "rp"))
	symlinkFile(t, "rp", filepath.Join(root, "bin", "rc"))

	inv := inspectRCInstallations(rp, filepath.Dir(rp), "linux")
	if inv.Installations != 1 || inv.HasDuplicates || inv.RunningShadowed || inv.SelectedPath != rp {
		t.Fatalf("rc symlink beside rp must be one install: %#v", inv)
	}
}

func TestInspectRCInstallationsWindowsRcCopyIsAlias(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Programs", "rp")
	rp := executableFile(t, filepath.Join(dir, "rp.exe"))
	rc := executableFile(t, filepath.Join(dir, "rc.exe")) // byte-identical copy

	for _, running := range []string{rp, rc} {
		inv := inspectRCInstallations(running, dir, "windows")
		if inv.Installations != 1 || inv.HasDuplicates || inv.RunningShadowed {
			t.Fatalf("running %s: identical rc.exe copy must be an alias: %#v", filepath.Base(running), inv)
		}
	}

	if err := os.WriteFile(rc, []byte("stale"), 0o755); err != nil {
		t.Fatal(err)
	}
	if inv := inspectRCInstallations(rp, dir, "windows"); !inv.HasDuplicates {
		t.Fatalf("a stale rc.exe that differs is a real second install: %#v", inv)
	}
}

func TestInspectRCInstallationsLegacyRcOnly(t *testing.T) {
	rc := executableFile(t, filepath.Join(t.TempDir(), "bin", "rc"))
	inv := inspectRCInstallations(rc, filepath.Dir(rc), "linux")
	if inv.Installations != 1 || inv.HasDuplicates || inv.RunningShadowed || inv.SelectedPath != rc {
		t.Fatalf("a pre-rename lone rc must be a healthy single install: %#v", inv)
	}
}

func TestInspectRCInstallationsStaleRcElsewhereIsDuplicate(t *testing.T) {
	root := t.TempDir()
	rp := executableFile(t, filepath.Join(root, "new", "rp"))
	symlinkFile(t, "rp", filepath.Join(root, "new", "rc"))
	stale := executableFile(t, filepath.Join(root, "old", "rc"))
	pathValue := strings.Join([]string{filepath.Dir(stale), filepath.Dir(rp)}, string(os.PathListSeparator))

	inv := inspectRCInstallations(rp, pathValue, "linux")
	if !inv.HasDuplicates || inv.SelectedPath != rp {
		t.Fatalf("an old rc shadowing the alias must be reported: %#v", inv)
	}
}

func TestInstalledCask(t *testing.T) {
	for _, tc := range []struct{ list, want string }{
		{"rootcause-org/tap/rc\nfirefox\n", "rc"},
		{"rootcause-org/tap/rp\nrootcause-org/tap/rc\n", "rp"},
		{"replypen/tap/rp\n", "rp"},
		{"rootcause-org/replypen/rp\n", ""}, // the archived pre-rename rp CLI is not ours
		{"", ""},
	} {
		if got := installedCask(tc.list); got != tc.want {
			t.Errorf("installedCask(%q) = %q, want %q", tc.list, got, tc.want)
		}
	}
}

// ourBinary copies the running test binary: its build info carries this module path, which is what
// isReplypenGoBinary checks, so it stands in for a real rp/rc release binary.
func ourBinary(t *testing.T, path string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestStandaloneUpdateMigratesLoneRcToRp(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	rc := ourBinary(t, filepath.Join(dir, "rc"))

	target, err := installStandaloneUpdate(rc, []byte("new release"), "linux")
	if err != nil {
		t.Fatal(err)
	}
	if target != filepath.Join(dir, "rp") {
		t.Fatalf("target = %s", target)
	}
	if body, _ := os.ReadFile(target); string(body) != "new release" {
		t.Fatalf("rp body = %q", body)
	}
	if link, err := os.Readlink(rc); err != nil || link != "rp" {
		t.Fatalf("rc must become a symlink to rp: %q, %v", link, err)
	}
}

func TestStandaloneUpdateLeavesForeignRcAlone(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	rp := ourBinary(t, filepath.Join(dir, "rp"))
	foreign := executableFile(t, filepath.Join(dir, "rc")) // e.g. the Plan 9 shell

	if _, err := installStandaloneUpdate(rp, []byte("new release"), "linux"); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(foreign); string(body) != "test" {
		t.Fatalf("foreign rc was touched: %q", body)
	}
}

func TestStandaloneUpdateRefreshesWindowsRcCopy(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Programs", "rp")
	rp := ourBinary(t, filepath.Join(dir, "rp.exe"))
	ourBinary(t, filepath.Join(dir, "rc.exe"))

	if _, err := installStandaloneUpdate(rp, []byte("new release"), "windows"); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(filepath.Join(dir, "rc.exe")); string(body) != "new release" {
		t.Fatalf("rc.exe copy not refreshed: %q", body)
	}
}

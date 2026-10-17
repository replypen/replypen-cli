package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestInspectRCInstallationsDetectsMiseShadowingHomebrew(t *testing.T) {
	root := t.TempDir()
	mise := executableFile(t, filepath.Join(root, "bin", "mise"))
	shim := symlinkFile(t, mise, filepath.Join(root, ".local", "share", "mise", "shims", "rp"))
	running := executableFile(t, filepath.Join(root, ".local", "share", "mise", "installs", "go", "1.25.9", "bin", "rp"))
	brewBinary := executableFile(t, filepath.Join(root, "homebrew", "Caskroom", "rp", "1.1.3", "rp"))
	brewLink := symlinkFile(t, brewBinary, filepath.Join(root, "homebrew", "bin", "rp"))

	pathValue := strings.Join([]string{filepath.Dir(shim), filepath.Dir(brewLink), filepath.Dir(running)}, string(os.PathListSeparator))
	inv := inspectRCInstallations(running, pathValue, "darwin")

	if inv.SelectedPath != shim {
		t.Fatalf("selected = %q, want shim %q", inv.SelectedPath, shim)
	}
	if inv.CanonicalPath != resolvePath(brewBinary) {
		t.Fatalf("canonical = %q, want Homebrew binary %q", inv.CanonicalPath, resolvePath(brewBinary))
	}
	if inv.Installations != 2 || !inv.HasDuplicates {
		t.Fatalf("installations = %d, duplicates = %v; want 2, true", inv.Installations, inv.HasDuplicates)
	}
	if !inv.RunningShadowed {
		t.Fatal("expected the mise-dispatched Go binary to be shadowing canonical Homebrew")
	}
	if got := inv.Paths[0].Kind; got != installMiseShim {
		t.Fatalf("selected kind = %q, want %q", got, installMiseShim)
	}
	if got := inv.Paths[1].Kind; got != installHomebrew || !inv.Paths[1].Shadowed {
		t.Fatalf("Homebrew path = %#v, want shadowed Homebrew", inv.Paths[1])
	}
	if got := inv.Paths[2].Kind; got != installGo || !inv.Paths[2].Running {
		t.Fatalf("running path = %#v, want running Go install", inv.Paths[2])
	}
}

func TestInspectRCInstallationsCleanStandalone(t *testing.T) {
	running := executableFile(t, filepath.Join(t.TempDir(), "bin", "rp"))
	inv := inspectRCInstallations(running, filepath.Dir(running), "linux")

	if inv.Installations != 1 || inv.HasDuplicates || inv.RunningShadowed {
		t.Fatalf("unexpected inventory: %#v", inv)
	}
	if inv.SelectedPath != running || inv.CanonicalPath != resolvePath(running) {
		t.Fatalf("selected/canonical = %q/%q, want %q/%q", inv.SelectedPath, inv.CanonicalPath, running, resolvePath(running))
	}
}

func TestInspectRCInstallationsDeduplicatesSymlinksToSameBinary(t *testing.T) {
	root := t.TempDir()
	running := executableFile(t, filepath.Join(root, "install", "rp"))
	first := symlinkFile(t, running, filepath.Join(root, "first", "rp"))
	second := symlinkFile(t, running, filepath.Join(root, "second", "rp"))
	pathValue := strings.Join([]string{filepath.Dir(first), filepath.Dir(second)}, string(os.PathListSeparator))

	inv := inspectRCInstallations(running, pathValue, "linux")
	if inv.SelectedPath != first {
		t.Fatalf("selected = %q, want first PATH entry %q", inv.SelectedPath, first)
	}
	if inv.Installations != 1 || inv.HasDuplicates {
		t.Fatalf("same resolved binary counted more than once: %#v", inv)
	}
	if inv.RunningShadowed {
		t.Fatal("selected symlink resolves to the running binary; must not report shadowing")
	}
}

func TestInspectRCInstallationsIncludesRunningBinaryOutsidePATH(t *testing.T) {
	root := t.TempDir()
	selected := executableFile(t, filepath.Join(root, "selected", "rp"))
	running := executableFile(t, filepath.Join(root, "running", "rp"))

	inv := inspectRCInstallations(running, filepath.Dir(selected), "linux")
	if len(inv.Paths) != 2 || !inv.Paths[1].Running {
		t.Fatalf("running binary missing from inventory: %#v", inv.Paths)
	}
	if !inv.RunningShadowed || !inv.HasDuplicates {
		t.Fatalf("absolute invocation of non-selected copy must be diagnosed: %#v", inv)
	}
}

func TestInspectRCInstallationsTreatsSelectedMiseShimAsShadowingAbsoluteHomebrew(t *testing.T) {
	root := t.TempDir()
	mise := executableFile(t, filepath.Join(root, "bin", "mise"))
	shim := symlinkFile(t, mise, filepath.Join(root, "mise", "shims", "rp"))
	brewBinary := executableFile(t, filepath.Join(root, "homebrew", "Caskroom", "rp", "1.1.3", "rp"))
	brewLink := symlinkFile(t, brewBinary, filepath.Join(root, "homebrew", "bin", "rp"))
	pathValue := strings.Join([]string{filepath.Dir(shim), filepath.Dir(brewLink)}, string(os.PathListSeparator))

	inv := inspectRCInstallations(brewBinary, pathValue, "darwin")
	if !inv.RunningShadowed {
		t.Fatalf("absolute Homebrew invocation must not prove selected shim dispatch: %#v", inv)
	}
}

// TestPathInventoryIsSharedByUpdateAndDoctor: `rp self update` refuses and points at `rp self doctor`,
// so the two must never disagree about WHICH rp copies are on PATH. They share scanPathRC; only the
// classification differs (path shape vs build-info provenance).
func TestPathInventoryIsSharedByUpdateAndDoctor(t *testing.T) {
	root := t.TempDir()
	first := executableFile(t, filepath.Join(root, "a", "rp"))
	second := executableFile(t, filepath.Join(root, "b", "rp"))
	pathValue := strings.Join([]string{
		filepath.Dir(first), filepath.Dir(second), filepath.Dir(first), // duplicate dir must dedupe
	}, string(os.PathListSeparator))

	var update []string
	for _, p := range inspectRCInstallations(first, pathValue, runtime.GOOS).Paths {
		update = append(update, p.Path)
	}
	var doctor []string
	for _, b := range scanPathBinaries(pathValue, binaryInfo{Path: first}, runtime.GOOS) {
		doctor = append(doctor, b.Path)
	}
	want := []string{first, second}
	if !slices.Equal(update, want) || !slices.Equal(doctor, want) {
		t.Fatalf("update=%v doctor=%v, want both %v", update, doctor, want)
	}
}

// TestClassifyInstallPath pins the path-SHAPE classifier `rp self update` uses (no binary is read).
// Doctor's build-info provenance classifier is a different question; see TestClassifyInstall.
func TestClassifyInstallPath(t *testing.T) {
	cases := []struct {
		path, resolved string
		want           installKind
	}{
		{"/opt/homebrew/bin/rp", "/opt/homebrew/Caskroom/rp/1.1.3/rp", installHomebrew},
		{"/Users/pj/.local/share/mise/shims/rp", "/opt/homebrew/bin/mise", installMiseShim},
		{"/Users/pj/.local/share/mise/installs/go/1.25.9/bin/rp", "/Users/pj/.local/share/mise/installs/go/1.25.9/bin/rp", installGo},
		{`C:\Users\pj\go\bin\rp.exe`, `C:\Users\pj\go\bin\rp.exe`, installGo},
		{"/Users/pj/.local/bin/rp", "/Users/pj/.local/bin/rp", installStandalone},
	}
	for _, tc := range cases {
		if got := classifyInstallPath(tc.path, tc.resolved); got != tc.want {
			t.Errorf("classifyInstallPath(%q, %q) = %q, want %q", tc.path, tc.resolved, got, tc.want)
		}
	}
}

func executableFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func symlinkFile(t *testing.T, target, path string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	return path
}

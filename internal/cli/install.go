package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

type installKind string

const (
	installHomebrew   installKind = "Homebrew"
	installGo         installKind = "Go install"
	installStandalone installKind = "standalone"
	installMiseShim   installKind = "mise shim"
)

// rcInstallPath is one rp (or legacy rc) command reachable from PATH, or the actual running binary
// when a shim dispatches outside PATH. A shim is deliberately not an installation: mise's generic rp
// symlink is a selector, not another copy of the rp binary. Neither is an Alias: the rc copy that
// install.ps1 / self update keep next to rp.exe on Windows (on Unix the alias is a symlink, so it
// already collapses onto rp by resolved path).
type rcInstallPath struct {
	Path         string
	ResolvedPath string
	Kind         installKind
	Selected     bool
	Running      bool
	Shadowed     bool
	Shim         bool
	Alias        bool
}

type rcInstallInventory struct {
	RunningPath     string
	SelectedPath    string
	CanonicalPath   string
	Paths           []rcInstallPath
	Installations   int
	HasDuplicates   bool
	RunningShadowed bool
}

// inspectRCInstallations inventories rp in PATH order and always includes the actual running binary.
// It takes all host-dependent inputs explicitly so tests can model shadowing without changing the
// developer's PATH or executing arbitrary candidate binaries.
func inspectRCInstallations(runningPath, pathValue, goos string) rcInstallInventory {
	runningPath = absoluteClean(runningPath)
	runningResolved := resolvePath(runningPath)
	inv := rcInstallInventory{RunningPath: runningResolved}

	for _, cand := range scanPathRC(pathValue, goos) {
		kind := classifyInstallPath(cand.Path, cand.Resolved)
		entry := rcInstallPath{
			Path:         cand.Path,
			ResolvedPath: cand.Resolved,
			Kind:         kind,
			Selected:     cand.Selected,
			Shadowed:     !cand.Selected,
			Shim:         kind == installMiseShim,
			Alias:        cand.Alias,
		}
		entry.Running = samePath(cand.Path, runningPath) || samePath(cand.Resolved, runningResolved)
		if entry.Selected {
			inv.SelectedPath = cand.Path
		}
		inv.Paths = append(inv.Paths, entry)
	}

	// os.Executable reports the dispatched Go binary when rp was entered through a mise shim. Include
	// it even when its bin directory is absent from PATH in the current project context.
	runningSeen := false
	for i := range inv.Paths {
		if inv.Paths[i].Running {
			runningSeen = true
			break
		}
	}
	if !runningSeen {
		inv.Paths = append(inv.Paths, rcInstallPath{
			Path:         runningPath,
			ResolvedPath: runningResolved,
			Kind:         classifyInstallPath(runningPath, runningResolved),
			Running:      true,
			Shadowed:     inv.SelectedPath != "",
		})
	}

	inv.Installations = countDistinctInstallations(inv.Paths)
	inv.HasDuplicates = inv.Installations > 1
	inv.CanonicalPath = chooseCanonicalInstall(inv.Paths, goos)
	inv.RunningShadowed = runningIsShadowed(inv, runningResolved)
	return inv
}

// pathRCCandidate is one executable named rp or the legacy rc (.exe on Windows) reachable from PATH,
// in PATH order. Selected marks the copy a bare `rp` would run — or, when no rp is on PATH at all (a
// pre-rename install), the copy a bare `rc` would run. Alias marks an rc sitting next to an rp.
type pathRCCandidate struct {
	Path     string
	Resolved string
	Selected bool
	Alias    bool
}

// scanPathRC is the ONE PATH inventory in the self-update/doctor plane: it walks pathValue in order,
// keeps every executable rp, dedupes by absolute path and resolves symlinks. Deliberately it does not
// classify — the two consumers ask different questions of the same list. `rp self update`
// (inspectRCInstallations) classifies by path SHAPE (classifyInstallPath: homebrew/go/mise-shim/
// standalone) to decide what shadows what, without reading any binary. `rp self doctor`
// (scanPathBinaries) reads each binary's embedded build info to report PROVENANCE (classifyInstall:
// release/go-install/source/homebrew). Keep the walk shared; keep the two classifications apart.
func scanPathRC(pathValue, goos string) []pathRCCandidate {
	seen := make(map[string]bool)
	out := make([]pathRCCandidate, 0)
	firstPrimary, firstLegacy := -1, -1
	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			dir = "."
		}
		primary := absoluteClean(filepath.Join(dir, binaryName(goos)))
		hasPrimary := isExecutableFile(primary, goos)
		for _, name := range []string{binaryName(goos), legacyBinaryName(goos)} {
			candidate := absoluteClean(filepath.Join(dir, name))
			if seen[candidate] || !isExecutableFile(candidate, goos) {
				continue
			}
			seen[candidate] = true
			legacy := name != binaryName(goos)
			if !legacy && firstPrimary < 0 {
				firstPrimary = len(out)
			}
			if legacy && firstLegacy < 0 {
				firstLegacy = len(out)
			}
			resolved := resolvePath(candidate)
			out = append(out, pathRCCandidate{
				Path:     candidate,
				Resolved: resolved,
				Alias:    legacy && hasPrimary && isAliasOf(resolved, resolvePath(primary), goos),
			})
		}
	}
	if firstPrimary >= 0 {
		out[firstPrimary].Selected = true
	} else if firstLegacy >= 0 {
		out[firstLegacy].Selected = true
	}
	return out
}

// isAliasOf: on Unix the rc alias is a symlink onto rp; on Windows it is a byte-identical copy. A stale
// rc that differs is a real second installation, and stays visible as one.
func isAliasOf(alias, primary, goos string) bool {
	if samePath(alias, primary) {
		return true
	}
	if goos != "windows" {
		return false
	}
	a, errA := os.ReadFile(alias)
	b, errB := os.ReadFile(primary)
	return errA == nil && errB == nil && bytes.Equal(a, b)
}

func absoluteClean(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	return filepath.Clean(path)
}

func resolvePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return absoluteClean(resolved)
	}
	return absoluteClean(path)
}

func isExecutableFile(path, goos string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}
	return goos == "windows" || info.Mode().Perm()&0o111 != 0
}

func classifyInstallPath(path, resolved string) installKind {
	if isMiseShim(path, resolved) {
		return installMiseShim
	}
	if isHomebrewManaged(resolved) || isHomebrewManaged(path) {
		return installHomebrew
	}
	if isGoInstallPath(resolved) || isGoInstallPath(path) || isReplypenGoBinary(resolved) {
		return installGo
	}
	return installStandalone
}

func isMiseShim(path, resolved string) bool {
	clean := filepath.ToSlash(path)
	if strings.Contains(clean, "/mise/shims/") {
		return true
	}
	return filepath.Base(resolved) == "mise"
}

func isGoInstallPath(path string) bool {
	clean := strings.ReplaceAll(filepath.ToSlash(path), `\`, "/")
	isRC := false
	for _, suffix := range []string{"/rp", "/rp.exe", "/rc", "/rc.exe"} {
		isRC = isRC || strings.HasSuffix(clean, suffix)
	}
	miseGoBin := strings.Contains(clean, "/mise/installs/go/") && strings.Contains(clean, "/bin/")
	standardGoBin := strings.Contains(clean, "/go/bin/")
	return isRC && (miseGoBin || standardGoBin)
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func countDistinctInstallations(paths []rcInstallPath) int {
	seen := make(map[string]bool)
	for _, path := range paths {
		if path.Shim || path.Alias {
			continue
		}
		key := path.ResolvedPath
		if !seen[key] {
			seen[key] = true
		}
	}
	return len(seen)
}

func chooseCanonicalInstall(paths []rcInstallPath, goos string) string {
	if goos == "darwin" {
		for _, path := range paths {
			if !path.Shim && path.Kind == installHomebrew {
				return path.ResolvedPath
			}
		}
	}
	for _, path := range paths {
		if path.Selected && !path.Shim {
			return path.ResolvedPath
		}
	}
	for _, path := range paths {
		if path.Running && !path.Shim {
			return path.ResolvedPath
		}
	}
	return ""
}

func runningIsShadowed(inv rcInstallInventory, runningResolved string) bool {
	if inv.SelectedPath == "" {
		return true
	}
	for _, path := range inv.Paths {
		if !path.Selected {
			continue
		}
		if path.Shim {
			// os.Executable cannot prove whether this process came through the shim or an absolute path.
			// Stay conservative: a selected shim is healthy only after migration removes it and PATH selects
			// the canonical binary directly.
			return true
		}
		if samePath(path.ResolvedPath, runningResolved) {
			return false
		}
		// Invoked through the Windows rc copy that sits beside the selected rp: same installation.
		for _, alias := range inv.Paths {
			if alias.Alias && alias.Running && samePath(filepath.Dir(alias.Path), filepath.Dir(path.Path)) {
				return false
			}
		}
		return true
	}
	return true
}

func findRunningInstall(paths []rcInstallPath) rcInstallPath {
	for _, path := range paths {
		if path.Running && !path.Shim {
			return path
		}
	}
	return rcInstallPath{Kind: installStandalone}
}

func findSelectedInstall(paths []rcInstallPath) *rcInstallPath {
	for i := range paths {
		if paths[i].Selected {
			return &paths[i]
		}
	}
	return nil
}

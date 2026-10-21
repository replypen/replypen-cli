package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// platformSkillsPull is what `rp dev platform-skills pull -o json` prints; the brain kit's docker gate
// mounts Dir/<name> at /skills/<name>.
type platformSkillsPull struct {
	Digest     string   `json:"digest"`
	Dir        string   `json:"dir"`
	Skills     []string `json:"skills"`
	Downloaded bool     `json:"downloaded"`
	Verified   bool     `json:"verified"` // false = --offline: the cached snapshot, freshness not checked
}

func newPlatformSkillsCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "platform-skills", Short: "Pull the platform skills runs mount at /skills"}
	var cacheDir string
	var offline bool
	pull := &cobra.Command{
		Use:   "pull",
		Short: "Cache the deployed platform skill library locally (a no-op when already current)",
		Long: "Fetches the manifest of the platform skill library the host mounts at /skills/<name> in runs and,\n" +
			"when its digest is not cached yet, downloads and verifies the archive into <cache>/<digest>/.\n" +
			"The brain kit's docker gate (brain_test.py / brain_run.py --mode docker) mounts that directory.\n" +
			"--offline prints the last pulled snapshot without contacting the host.",
		Example: "  rp dev platform-skills pull\n  rp dev platform-skills pull -o json\n  rp dev platform-skills pull --offline",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			root, err := platformSkillsRoot(cacheDir)
			if err != nil {
				return err
			}
			var out *platformSkillsPull
			if offline {
				out, err = cachedPlatformSkills(root)
			} else {
				out, err = pullPlatformSkills(e, root)
			}
			if err != nil {
				return err
			}
			if e.jsonOut() {
				return writeJSON(e, out)
			}
			state := "current"
			switch {
			case !out.Verified:
				state = "cached, not checked against the host (--offline)"
			case out.Downloaded:
				state = "downloaded"
			}
			_, err = fmt.Fprintf(e.out, "platform skills %s (%s): %s\n  %s\n", out.Digest[:12], state,
				strings.Join(out.Skills, ", "), out.Dir)
			return err
		},
	}
	pull.Flags().StringVar(&cacheDir, "cache-dir", "", "cache root (default <user cache dir>/replypen/platform-skills)")
	pull.Flags().BoolVar(&offline, "offline", false, "print the last pulled snapshot; never contact the host")
	cmd.AddCommand(pull)
	return cmd
}

func platformSkillsRoot(flag string) (string, error) {
	if flag != "" {
		return filepath.Abs(flag)
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("no user cache dir: %w (pass --cache-dir)", err)
	}
	return filepath.Join(base, "replypen", "platform-skills"), nil
}

func pullPlatformSkills(e *env, root string) (*platformSkillsPull, error) {
	c, err := e.newClient()
	if err != nil {
		return nil, err
	}
	manifest, _, err := c.PlatformSkillsManifest(e.ctx())
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, manifest.Digest)
	downloaded := false
	if _, statErr := os.Stat(dir); statErr != nil {
		var archive bytes.Buffer
		if err := c.PlatformSkillsArchive(e.ctx(), &archive); err != nil {
			return nil, err
		}
		got, err := extractPlatformSkills(archive.Bytes(), root, manifest.Digest)
		if err != nil {
			return nil, err
		}
		dir, downloaded = filepath.Join(root, got), true
	}
	digest := filepath.Base(dir)
	if err := os.WriteFile(filepath.Join(root, "current"), []byte(digest+"\n"), 0o644); err != nil {
		return nil, err
	}
	return &platformSkillsPull{Digest: digest, Dir: dir, Skills: skillNames(dir), Downloaded: downloaded, Verified: true}, nil
}

func cachedPlatformSkills(root string) (*platformSkillsPull, error) {
	raw, err := os.ReadFile(filepath.Join(root, "current"))
	if err != nil {
		return nil, fmt.Errorf("no platform skills cached under %s: run `rp dev platform-skills pull` online first", root)
	}
	digest := strings.TrimSpace(string(raw))
	dir := filepath.Join(root, digest)
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("cached platform skills %s missing: run `rp dev platform-skills pull`", dir)
	}
	return &platformSkillsPull{Digest: digest, Dir: dir, Skills: skillNames(dir)}, nil
}

// extractPlatformSkills unpacks the archive into root/<digest>, where digest is recomputed from the
// files exactly as the host computes it (skillcatalog.TakeSnapshot) — so the directory name proves its
// content. A deploy between manifest and archive just yields the newer digest. Atomic: a temp dir is
// renamed into place, so a crashed pull never leaves a half snapshot under a digest name.
func extractPlatformSkills(archive []byte, root, want string) (string, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(root, ".pull-")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return "", fmt.Errorf("platform skills archive: %w", err)
	}
	type entry struct {
		rel  string
		mode int64
		sum  [32]byte
	}
	var entries []entry
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("platform skills archive: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			return "", fmt.Errorf("platform skills archive: unexpected entry %q (type %c)", h.Name, h.Typeflag)
		}
		rel := path.Clean(h.Name)
		if path.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") || rel == "." {
			return "", fmt.Errorf("platform skills archive: unsafe path %q", h.Name)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return "", err
		}
		mode := int64(0o644)
		if h.Mode&0o111 != 0 {
			mode = 0o755
		}
		dst := filepath.Join(tmp, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(dst, body, os.FileMode(mode)); err != nil {
			return "", err
		}
		entries = append(entries, entry{rel: rel, mode: mode, sum: sha256.Sum256(body)})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	sum := sha256.New()
	for _, en := range entries {
		_, _ = fmt.Fprintf(sum, "%s\x00%o\x00%x\n", en.rel, en.mode, en.sum)
	}
	digest := hex.EncodeToString(sum.Sum(nil))
	if want != "" && digest != want {
		// The host redeployed between the two requests: keep the archive's own (newer) digest.
		_, _ = fmt.Fprintf(os.Stderr, "note: platform skills changed mid-pull (%s → %s)\n", want[:12], digest[:12])
	}
	dst := filepath.Join(root, digest)
	if _, err := os.Stat(dst); err == nil {
		return digest, nil
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	return digest, nil
}

func skillNames(dir string) []string {
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, en := range entries {
		if en.IsDir() && !strings.HasPrefix(en.Name(), ".") {
			names = append(names, en.Name())
		}
	}
	return names
}

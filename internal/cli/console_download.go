package cli

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rootcause-org/rootcause-cli/internal/client"
	"github.com/rootcause-org/rootcause-cli/internal/outputspill"
	"github.com/rootcause-org/rootcause-cli/internal/render"
)

// sandboxFileRoots mirror the host's console file allowlist: only these stream directly, anything else
// (a directory, /kb, /brain, /mirrors) is staged into /tmp by one bash run first.
var sandboxFileRoots = []string{"/tmp/", "/home/agent/"}

func newConsoleFileCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "file", Short: "Transfer files from a guarded workspace console"}
	var out string
	var principalKind, principalID string
	get := &cobra.Command{
		Use:     "get <sandbox-path> [local-path]",
		Aliases: []string{"download"},
		Short:   "Download a sandbox file or directory without the bash output cap",
		Long: "Download a file or directory from the console sandbox, any size (up to 512 MiB), byte-exact.\n" +
			"Directories arrive as a local directory (tar'ed in the sandbox, unpacked here). Paths outside\n" +
			"/tmp and /home/agent (e.g. /kb, /brain, /mirrors) are staged through /tmp transparently.\n" +
			"local-path defaults to the sandbox path's base name in the current directory; - writes a file to stdout.",
		Example: "  rc dev console file get /kb/knowledgeowl ./kb\n" +
			"  rc dev console file get /tmp/rc-console/stdout-7.txt ./stdout.txt",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			remote := args[0]
			if !strings.HasPrefix(remote, "/") {
				return fmt.Errorf("sandbox path must be absolute (got %q)", remote)
			}
			local := out
			if len(args) == 2 {
				if out != "" {
					return fmt.Errorf("give the local path either as an argument or with --out, not both")
				}
				local = args[1]
			}
			if local == "auto" {
				return fmt.Errorf("file get requires an explicit local path or -")
			}
			if local == "" {
				local = path.Base(path.Clean(remote))
				if local == "/" || local == "." {
					return fmt.Errorf("give a local path for %s", remote)
				}
				if _, err := os.Lstat(local); err == nil {
					return fmt.Errorf("%s already exists; pass a local path explicitly to overwrite a file", local)
				}
			}
			principal, err := principalFromFlags(principalKind, principalID, "", "")
			if err != nil {
				return err
			}
			c, err := e.newClient()
			if err != nil {
				return err
			}
			d := sandboxDownload{e: e, c: c, principal: principal}
			if underSandboxFileRoots(remote) && !strings.HasSuffix(remote, "/") {
				err := d.getFile(remote, local)
				var apiErr *client.APIError
				if err == nil || !asAPIError(err, &apiErr) || apiErr.Code != "FILE_READ_FAILED" {
					return err
				}
				// Not a regular file (a directory?) or missing: staging tells which, with a real error.
			}
			return d.staged(remote, local)
		},
	}
	get.Flags().StringVar(&out, "out", "", "local destination path, or - for stdout (same as the local-path argument)")
	addConsolePrincipalFlags(get, &principalKind, &principalID)
	cmd.AddCommand(get)
	return cmd
}

func underSandboxFileRoots(p string) bool {
	clean := path.Clean(p) + "/"
	for _, root := range sandboxFileRoots {
		if strings.HasPrefix(clean, root) && clean != root {
			return true
		}
	}
	return false
}

type sandboxDownload struct {
	e         *env
	c         *client.Client
	principal *client.Principal
}

func (d sandboxDownload) getFile(remote, local string) error {
	if local == "-" {
		return d.c.FileGet(d.e.ctx(), remote, d.e.scopeProject(), d.e.scopeTenant(), d.principal, d.e.out)
	}
	target, err := openOutputTarget(d.e, local, "", "binary")
	if err != nil {
		return err
	}
	if err := d.c.FileGet(d.e.ctx(), remote, d.e.scopeProject(), d.e.scopeTenant(), d.principal, target.Writer()); err != nil {
		target.abort()
		return err
	}
	return target.finish(d.e, false)
}

// stageScript copies $1 into /tmp (a directory as a tar.gz of its contents) unless it is already a
// streamable regular file, and prints "<file|dir>\t<sandbox path>[\tstaged]". Prefixed with p=<path>.
const stageScript = `
if [ -d "$p" ]; then
  out=$(mktemp -p /tmp rc-download-XXXXXX) && tar --hard-dereference -czf "$out" -C "$p" . && printf 'dir\t%s\tstaged\n' "$out"
elif [ -f "$p" ]; then
  r=$(readlink -f -- "$p")
  case "$r" in
    /tmp/*|/home/agent/*) printf 'file\t%s\n' "$r" ;;
    *) out=$(mktemp -p /tmp rc-download-XXXXXX) && cp -- "$p" "$out" && printf 'file\t%s\tstaged\n' "$out" ;;
  esac
else
  echo "no such file or directory in the sandbox: $p" >&2; exit 2
fi`

func (d sandboxDownload) staged(remote, local string) error {
	resp, err := d.bash("p=" + shellQuote(remote) + stageScript)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.TrimRight(resp.Stdout, "\n"), "\n")
	fields := strings.Split(lines[len(lines)-1], "\t")
	if resp.ExitCode != 0 || len(fields) < 2 {
		msg := strings.TrimSpace(resp.Stderr)
		if msg == "" {
			msg = fmt.Sprintf("staging %s failed (exit %d)", remote, resp.ExitCode)
		}
		return fmt.Errorf("%s", msg)
	}
	kind, sandboxPath, staged := fields[0], fields[1], len(fields) > 2
	if staged {
		defer func() { _, _ = d.bash("rm -f -- " + shellQuote(sandboxPath)) }()
	}
	if kind == "file" {
		return d.getFile(sandboxPath, local)
	}
	if local == "-" {
		return fmt.Errorf("%s is a directory; give a local directory path instead of -", remote)
	}
	return d.getDir(sandboxPath, remote, local)
}

func (d sandboxDownload) bash(command string) (*client.BashRunResponse, error) {
	resp, _, err := d.c.BashRun(d.e.ctx(), client.BashRunRequest{Command: command, Principal: d.principal}, d.e.scopeProject(), d.e.scopeTenant())
	return resp, err
}

// getDir streams the staged tarball into a sibling temp dir and renames it into place, so local is
// either the complete tree or absent.
func (d sandboxDownload) getDir(tarball, remote, local string) error {
	if _, err := os.Lstat(local); err == nil {
		return fmt.Errorf("%s already exists; directories are never merged into an existing path", local)
	}
	parent := filepath.Dir(local)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, ".rc-download-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	var st extractStats
	go func() {
		var err error
		st, err = extractTarGz(pr, tmp)
		_ = pr.CloseWithError(err)
		done <- err
	}()
	getErr := d.c.FileGet(d.e.ctx(), tarball, d.e.scopeProject(), d.e.scopeTenant(), d.principal, pw)
	_ = pw.CloseWithError(getErr)
	if err := <-done; getErr == nil && err != nil {
		getErr = fmt.Errorf("unpack %s: %w", remote, err)
	}
	if getErr != nil {
		return getErr
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, local); err != nil {
		return fmt.Errorf("install %s: %w", local, err)
	}
	if d.e.jsonOut() {
		enc := json.NewEncoder(d.e.out)
		enc.SetIndent("", "  ")
		return enc.Encode(map[string]any{"path": local, "kind": "dir", "files": st.files, "bytes": st.bytes, "skipped": st.skipped})
	}
	_, err = fmt.Fprintf(d.e.out, "downloaded %s → %s/ (%d files, %d bytes", remote, local, st.files, st.bytes)
	if st.skipped > 0 {
		_, _ = fmt.Fprintf(d.e.out, ", %d symlinks/special files skipped", st.skipped)
	}
	_, _ = fmt.Fprintln(d.e.out, ")")
	return err
}

type extractStats struct{ files, skipped, bytes int64 }

// extractTarGz unpacks regular files and directories into dst through an os.Root, so no entry can write
// outside it. Symlinks and special files are skipped and counted, never created: a remote archive must
// not plant links on the laptop (staging already resolves hardlinks into regular files).
func extractTarGz(r io.Reader, dst string) (extractStats, error) {
	var st extractStats
	root, err := os.OpenRoot(dst)
	if err != nil {
		return st, err
	}
	defer func() { _ = root.Close() }()
	gz, err := gzip.NewReader(r)
	if err != nil {
		return st, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			// Read on through gz, not r: the gzip CRC/size trailer follows tar's end-of-archive blocks and
			// is only checked at gzip EOF. This also drains the pipe so the download completes.
			if _, err := io.Copy(io.Discard, gz); err != nil {
				return st, err
			}
			return st, nil
		}
		if err != nil {
			return st, err
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "./"))
		if name == "." {
			continue
		}
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return st, fmt.Errorf("unsafe entry %q", h.Name)
		}
		target := filepath.FromSlash(name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := root.MkdirAll(target, 0o755); err != nil {
				return st, err
			}
		case tar.TypeReg:
			if dir := filepath.Dir(target); dir != "." {
				if err := root.MkdirAll(dir, 0o755); err != nil {
					return st, err
				}
			}
			f, err := root.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_EXCL, os.FileMode(h.Mode&0o755)|0o600)
			if err != nil {
				return st, err
			}
			n, err := io.Copy(f, tr)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return st, err
			}
			st.files++
			st.bytes += n
		default:
			st.skipped++
		}
	}
}

// stdoutTruncationHint is the copy-pasteable next step for a truncated bash run: the exact command that
// downloads the full stdout the host kept, or (older host) how to redirect and download.
func stdoutTruncationHint(e *env, r *client.BashRunResponse) string {
	if !r.StdoutTruncated {
		return ""
	}
	if r.StdoutPath == "" {
		return fmt.Sprintf("[stdout truncated at %s. To get all of it, redirect in the sandbox and download: "+
			"rc dev console bash run '<command> > /tmp/out.txt'%s && rc dev console file get /tmp/out.txt ./out.txt%s]",
			humanBytes(int64(len(r.Stdout))), consoleScopeFlags(e, r), consoleScopeFlags(e, r))
	}
	local := "./" + path.Base(r.StdoutPath)
	return fmt.Sprintf("[stdout truncated at %s of %s. Full stdout: rc dev console file get %s %s%s "+
		"(within ~2 min: the sandbox is wiped once the console session idles out)]",
		humanBytes(int64(len(r.Stdout))), humanBytes(r.StdoutBytes), r.StdoutPath, local, consoleScopeFlags(e, r))
}

// consoleScopeFlags reproduces the scope that answered, so a follow-up lands in the same warm session.
func consoleScopeFlags(e *env, r *client.BashRunResponse) string {
	var b strings.Builder
	if e.profile != "" {
		b.WriteString(" --profile " + shellQuote(e.profile))
	}
	if e.project != "" {
		b.WriteString(" --project " + shellQuote(e.project))
	}
	if r.Tenant != "" {
		b.WriteString(" --tenant " + shellQuote(r.Tenant))
	}
	if r.Principal != nil {
		b.WriteString(" --principal-kind " + shellQuote(r.Principal.Kind) + " --principal-id " + shellQuote(r.Principal.ExternalID))
	}
	return b.String()
}

// withJSONHint appends a top-level "hint" member to a JSON object without re-encoding (key order and
// number formatting stay exactly the server's).
func withJSONHint(raw json.RawMessage, hint string) json.RawMessage {
	if hint == "" {
		return raw
	}
	trimmed := strings.TrimRight(string(raw), " \t\r\n")
	if !strings.HasSuffix(trimmed, "}") {
		return raw
	}
	h, _ := json.Marshal(hint)
	body := strings.TrimSuffix(trimmed, "}")
	sep := ","
	if strings.TrimSpace(body) == "{" {
		sep = ""
	}
	return json.RawMessage(body + sep + `"hint":` + string(h) + "}")
}

// renderJSONHinted is renderJSON with hint also surfaced in a spill manifest's hints, so an agent reading
// either the inline JSON or the manifest sees it.
func (e *env) renderJSONHinted(label string, raw json.RawMessage, hint string) error {
	raw = withJSONHint(raw, hint)
	m, err := outputspill.MaybeSpillJSON(e.spillConfig(), label, raw)
	if err != nil {
		return err
	}
	if m == nil {
		return render.JSON(e.out, raw)
	}
	if hint != "" {
		m.Hints = append([]string{hint}, m.Hints...)
	}
	return outputspill.WriteManifest(e.out, *m)
}

func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./:=@") == "" {
		return s
	}
	return outputspill.ShellQuote(s)
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KiB", n>>10)
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

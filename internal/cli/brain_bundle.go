package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/config"
	"github.com/replypen/replypen-cli/internal/render"
)

// A brain without a remote (brain_remote "none") has no GitHub repository: ReplyPen holds it on box. A
// developer edits it with ordinary local git and moves commits as git bundles over the API: clone/pull
// download a bundle, push uploads one branch as dev/<name>, publish lands it on main (fast-forward only,
// behind the server's boot check) and promotes where asked. The checkout's origin is a bundle file in
// .git, refreshed by every pull, so plain `git fetch origin` / `git log origin/main` keep working.

const bundleFileName = "replypen.bundle"

// maxBundleUpload is the server's upload cap; a var only so a test can lower it.
var maxBundleUpload = client.MaxBrainBundleUpload

// devNameUnsafe is everything the server refuses in a dev/<name> segment.
var devNameUnsafe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func brainCloneCmd(e *env) *cobra.Command {
	var temp bool
	cmd := &cobra.Command{
		Use:   "clone <project> [dir]",
		Short: "Clone a brain that has no GitHub repository (add --tenant <slug> for a tenant brain)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			project := args[0]
			if temp && len(args) == 2 {
				return errors.New("--temp picks its own directory under the user cache; drop the dir argument")
			}
			if e.project == "" {
				e.project = project
			}
			c, err := e.newClient()
			if err != nil {
				return err
			}
			tenant := e.scopeTenant()
			dir := project
			if tenant != "" {
				dir += "-" + tenant
			}
			if len(args) == 2 {
				dir = args[1]
			}
			if temp {
				cache, err := os.UserCacheDir()
				if err != nil {
					return err
				}
				root := filepath.Join(cache, "replypen", "brains")
				if err := os.MkdirAll(root, 0o700); err != nil {
					return err
				}
				// Resolved, so the dir in this receipt equals the one push/publish receipts report.
				if root, err = filepath.EvalSymlinks(root); err != nil {
					return err
				}
				if dir, err = os.MkdirTemp(root, dir+"-"); err != nil {
					return err
				}
			}
			entries, rerr := os.ReadDir(dir)
			if rerr == nil && len(entries) > 0 {
				return fmt.Errorf("%s already exists and is not empty; pick another directory", dir)
			}
			if err := requireLocalOnlyBrain(e, c, project, tenant, "git clone it from GitHub instead"); err != nil {
				return err
			}
			abs, err := filepath.Abs(dir)
			if err != nil {
				return err
			}
			// The bundle is downloaded beside the target so moving it into .git is a same-filesystem rename
			// (from a tmpfs /tmp it would fail half way).
			tmp, err := os.MkdirTemp(filepath.Dir(abs), ".rp-brain-clone-")
			if err != nil {
				return err
			}
			defer func() { _ = os.RemoveAll(tmp) }()
			// A failed clone is removed again, so a rerun is not refused by its leftovers.
			created, done := os.IsNotExist(rerr) || temp, false
			defer func() {
				if !done && created {
					_ = os.RemoveAll(abs)
				}
			}()
			bundle := filepath.Join(tmp, bundleFileName)
			if err := downloadBundle(e, c, project, tenant, bundle); err != nil {
				return err
			}
			if _, err := gitIn(e.ctx(), "", "clone", "--quiet", "--branch", "main", "--origin", "origin", "--", bundle, dir); err != nil {
				return err
			}
			gitDir, err := gitIn(e.ctx(), dir, "rev-parse", "--absolute-git-dir")
			if err != nil {
				return err
			}
			kept := filepath.Join(gitDir, bundleFileName)
			if err := os.Rename(bundle, kept); err != nil {
				return err
			}
			if _, err := gitIn(e.ctx(), dir, "remote", "set-url", "origin", kept); err != nil {
				return err
			}
			wrote, err := writeCheckoutMarkers(dir, gitDir, project, tenant)
			if err != nil {
				return err
			}
			done = true
			mainSHA, _ := gitIn(e.ctx(), dir, "rev-parse", "main")
			if e.jsonOut() {
				receipt := map[string]any{"project": project, "tenant": tenant, "dir": dir, "main": mainSHA, "sha": mainSHA}
				if temp {
					receipt["temp"] = true
				}
				raw, merr := json.Marshal(receipt)
				if merr != nil {
					return merr
				}
				return e.renderJSONInline(raw)
			}
			_, _ = fmt.Fprintf(e.out, "Cloned the %s brain into %s (main %s).\n", brainScopeLabel(project, tenant), dir, short(mainSHA))
			if wrote != "" {
				_, _ = fmt.Fprintf(e.out, "Wrote %s so rp knows this checkout; it rides along with your first commit.\n", wrote)
			}
			if temp {
				_, _ = fmt.Fprintf(e.out, "Next: edit on main, then `rp dev brain publish -m \"<what and why>\" --rm` (or `rp dev brain push -m \"<what and why>\"` to test it first with %s dev/<name>).\n", brainRefFlag(tenant))
				return nil
			}
			_, _ = fmt.Fprintf(e.out, "Next: create a branch, commit, then `rp dev brain publish` (or `rp dev brain push` to test it first with %s dev/<branch>).\n", brainRefFlag(tenant))
			return nil
		},
	}
	cmd.Flags().BoolVar(&temp, "temp", false, "throwaway checkout under the user cache dir (no dir argument); delete it after publishing with publish --rm")
	return cmd
}

// brainRefFlag is the `rp ask` flag that tests an uploaded dev/ ref of this brain.
func brainRefFlag(tenant string) string {
	if tenant != "" {
		return "--tenant-brain-ref"
	}
	return "--brain-ref"
}

func brainPullCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Fetch the brain's latest main and branches into this checkout (brains without a GitHub repository)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			co, c, err := openBundleCheckout(e, "use `git pull`")
			if err != nil {
				return err
			}
			before, after, kept, mainAt, err := pullBundle(e, c, co)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				raw, merr := json.Marshal(map[string]any{"project": co.project, "tenant": co.tenant, "main_before": before, "main": after, "local_main_kept": kept, "main_checked_out_in": mainAt})
				if merr != nil {
					return merr
				}
				return e.renderJSONInline(raw)
			}
			switch {
			case mainAt != "":
				_, _ = fmt.Fprintf(e.out, "main is checked out in %s, so it was left as is: run `rp dev brain pull` there (origin/main is now %s).\n", mainAt, short(after))
			case kept:
				_, _ = fmt.Fprintf(e.out, "Your local main has commits the brain's main does not; it was left as is. Move them to a branch, then `git reset --keep origin/main` on main.\n")
			case before == after:
				_, _ = fmt.Fprintf(e.out, "main is up to date (%s).\n", short(after))
			default:
				_, _ = fmt.Fprintf(e.out, "main: %s → %s\n", short(before), short(after))
			}
			if cur := currentBranch(e.ctx(), co.dir); cur != "" && cur != "main" {
				if gitIsAncestorIn(e.ctx(), co.dir, "origin/main", "HEAD") != nil {
					_, _ = fmt.Fprintf(e.out, "Your branch %s does not contain the new main: `git rebase main` before you publish.\n", cur)
				}
			}
			return nil
		},
	}
}

func brainPushCmd(e *env) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "push [name]",
		Short: "Upload the current branch as dev/<name> so test runs can use it (brains without a GitHub repository)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			co, c, err := openBundleCheckout(e, "use `git push`")
			if err != nil {
				return err
			}
			if err := commitPending(e, co, message); err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			up, err := pushBundle(e, c, co, name)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				raw, merr := json.Marshal(up)
				if merr != nil {
					return merr
				}
				return e.renderJSONInline(raw)
			}
			if up.Ref == "" {
				_, _ = fmt.Fprintf(e.out, "Nothing to upload: %s is already on the brain's main.\n", short(up.SHA))
				return nil
			}
			_, _ = fmt.Fprintf(e.out, "Uploaded as %s: %s\n", up.Ref, up.SHA)
			_, _ = fmt.Fprintf(e.out, "Test it with %s %s; make it live with `rp dev brain publish`.\n", brainRefFlag(co.tenant), up.Ref)
			return nil
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "commit uncommitted changes (git add -A) with this message first; without it a dirty tree is refused")
	return cmd
}

// commitPending commits the whole working tree (untracked files included) with message. Without a message
// a dirty tree is refused: uploading HEAD while edits sit uncommitted would publish a partial tree. The
// marker clone writes, left untracked, is not an edit: alone it is neither refused nor committed.
func commitPending(e *env, co bundleCheckout, message string) error {
	status, err := gitIn(e.ctx(), co.dir, "status", "--porcelain")
	if err != nil || status == "" || status == "?? "+config.MarkerFileName {
		return err
	}
	if err := assertCheckoutScope(co); err != nil {
		return err
	}
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("%s has uncommitted changes: pass -m \"<what and why>\" to commit them all, or commit (or discard) them yourself first", co.dir)
	}
	return commitAll(e, co, message)
}

func commitAll(e *env, co bundleCheckout, message string) error {
	if _, err := gitIn(e.ctx(), co.dir, "add", "-A"); err != nil {
		return err
	}
	_, err := gitIn(e.ctx(), co.dir, "commit", "--quiet", "-m", message)
	return err
}

// bundleCheckout is a local brain checkout bound to its project/tenant scope.
type bundleCheckout struct {
	dir, gitDir, project, tenant string
}

// openBundleCheckout resolves the checkout in the current directory and its scope, and refuses a brain
// that lives on GitHub (githubHint is the plain-git alternative).
func openBundleCheckout(e *env, githubHint string) (bundleCheckout, *client.Client, error) {
	dir, err := gitIn(e.ctx(), "", "rev-parse", "--show-toplevel")
	if err != nil {
		return bundleCheckout{}, nil, errors.New("not inside a brain checkout: run `rp dev brain clone <project>` first")
	}
	gitDir, err := gitIn(e.ctx(), dir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return bundleCheckout{}, nil, err
	}
	c, err := e.newClient()
	if err != nil {
		return bundleCheckout{}, nil, err
	}
	if err := e.resolvePinnedProject(c); err != nil {
		return bundleCheckout{}, nil, err
	}
	co := bundleCheckout{dir: dir, gitDir: gitDir, project: e.scopeProject(), tenant: e.scopeTenant()}
	if err := requireLocalOnlyBrain(e, c, co.project, co.tenant, githubHint); err != nil {
		return bundleCheckout{}, nil, err
	}
	return co, c, nil
}

func requireLocalOnlyBrain(e *env, c *client.Client, project, tenant, githubHint string) error {
	st, _, err := c.BrainStatus(e.ctx(), project, tenant)
	if err != nil {
		return err
	}
	if src := st.Status.Source; src != nil {
		return brainAliasError(&client.APIError{Status: http.StatusConflict, Code: brainReadOnlyAlias,
			Message: "this project runs on a read-only brain alias"}, project, src.Project)
	}
	if st.Status.RemoteMode != remoteModeNone {
		return fmt.Errorf("the %s brain lives on GitHub: %s", brainScopeLabel(project, tenant), githubHint)
	}
	return nil
}

// pullBundle downloads a fresh bundle into .git, fetches it as origin and fast-forwards local main. A
// local main with commits the server's main lacks is never moved (kept=true), nor is a main checked out
// in another worktree (mainAt names it): moving the ref under it would leave its index and files stale.
func pullBundle(e *env, c *client.Client, co bundleCheckout) (before, after string, kept bool, mainAt string, err error) {
	if after, err = fetchBundle(e, c, co); err != nil {
		return "", "", false, "", err
	}
	before, _ = gitIn(e.ctx(), co.dir, "rev-parse", "--verify", "--quiet", "refs/heads/main")
	switch {
	case before == after:
		return before, after, false, "", nil
	case before == "":
		_, err = gitIn(e.ctx(), co.dir, "branch", "--quiet", "main", "refs/remotes/origin/main")
		return before, after, false, "", err
	case gitIsAncestorIn(e.ctx(), co.dir, before, after) != nil:
		return before, before, true, "", nil
	case currentBranch(e.ctx(), co.dir) == "main":
		_, err = gitIn(e.ctx(), co.dir, "merge", "--ff-only", "--quiet", "refs/remotes/origin/main")
	default:
		if at := worktreeWithMain(e.ctx(), co.dir); at != "" {
			return before, before, false, at, nil
		}
		_, err = gitIn(e.ctx(), co.dir, "update-ref", "refs/heads/main", after, before)
	}
	return before, after, false, "", err
}

// fetchBundle downloads a fresh bundle into .git and fetches it as origin; local branches are not touched.
// Returns the new origin/main.
func fetchBundle(e *env, c *client.Client, co bundleCheckout) (string, error) {
	bundlePath := filepath.Join(co.gitDir, bundleFileName)
	if err := downloadBundle(e, c, co.project, co.tenant, bundlePath); err != nil {
		return "", err
	}
	if _, err := gitIn(e.ctx(), co.dir, "remote", "set-url", "origin", bundlePath); err != nil {
		return "", err
	}
	if _, err := gitIn(e.ctx(), co.dir, "fetch", "--quiet", "--prune", "--tags", "origin", "+refs/heads/*:refs/remotes/origin/*"); err != nil {
		return "", err
	}
	after, err := gitIn(e.ctx(), co.dir, "rev-parse", "refs/remotes/origin/main")
	if err != nil {
		return "", errors.New("the downloaded brain has no main")
	}
	return after, nil
}

// worktreeWithMain is the path of another worktree of this repository that has main checked out ("" =
// none). Called only when this worktree is not on main.
func worktreeWithMain(ctx context.Context, dir string) string {
	out, err := gitIn(ctx, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return ""
	}
	path := ""
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			path = p
		} else if line == "branch refs/heads/main" {
			return path
		}
	}
	return ""
}

// bundlePush is push's receipt. Ref is empty when HEAD is already on the server's main (nothing to upload).
type bundlePush struct {
	Project string `json:"project"`
	Tenant  string `json:"tenant,omitempty"`
	Dir     string `json:"dir"`
	Branch  string `json:"branch"`
	Ref     string `json:"ref,omitempty"`
	SHA     string `json:"sha"`
}

// pushBundle bundles the current branch against origin/main (which the server always has) and uploads
// it as dev/<name>; name defaults to the branch name made safe.
func pushBundle(e *env, c *client.Client, co bundleCheckout, name string) (bundlePush, error) {
	branch := currentBranch(e.ctx(), co.dir)
	if branch == "" {
		return bundlePush{}, errors.New("HEAD is detached: check out a branch first (`git switch -c <name>`)")
	}
	head, err := gitIn(e.ctx(), co.dir, "rev-parse", "HEAD")
	if err != nil {
		return bundlePush{}, err
	}
	up := bundlePush{Project: co.project, Tenant: co.tenant, Dir: co.dir, Branch: branch, SHA: head}
	base, err := gitIn(e.ctx(), co.dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/main")
	if err != nil || base == "" {
		return up, errors.New("this checkout has no origin/main: run `rp dev brain pull` first")
	}
	if gitIsAncestorIn(e.ctx(), co.dir, head, base) == nil {
		return up, nil
	}
	if err := assertCheckoutScope(co); err != nil {
		return up, err
	}
	if name == "" {
		name = devName(branch, head)
	}
	tmp, err := os.MkdirTemp("", "rp-brain-push-")
	if err != nil {
		return up, err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	path := filepath.Join(tmp, "push.bundle")
	if _, err := gitIn(e.ctx(), co.dir, "bundle", "create", "--quiet", path, "refs/heads/"+branch, "^"+base); err != nil {
		return up, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return up, err
	}
	if fi.Size() > maxBundleUpload {
		return up, fmt.Errorf("your branch bundles to %d MB, over the %d MB upload limit: keep large files out of the brain, or split the change into smaller branches",
			fi.Size()>>20, maxBundleUpload>>20)
	}
	resp, _, err := c.UploadBrainRef(e.ctx(), co.project, co.tenant, name, "refs/heads/"+branch, path)
	if err != nil {
		return up, err
	}
	up.Ref, up.SHA = resp.Ref, resp.SHA
	return up, nil
}

// devName turns a local branch into a server-safe dev/<name> segment; main/channel names (refused by the
// server) and names that sanitize to nothing fall back to edit-<short sha>.
func devName(branch, sha string) string {
	n := strings.Trim(devNameUnsafe.ReplaceAllString(branch, "-"), "-.")
	switch strings.ToLower(n) {
	case "", "main", "stable", "edge", "head", "dev", "master":
		return "edit-" + short(sha)
	}
	if len(n) > 100 {
		n = n[:100]
	}
	return n
}

// publishLocalBrain is `rp dev brain publish` without --sha on a brain without a remote: upload the
// checkout's branch, fast-forward the server's main to it (boot-checked), then promote --channel if given.
// A main that moved meanwhile is replayed onto (disjoint paths only), and the checkout is refreshed after.
func publishLocalBrain(e *env, channel, message string, remove bool) error {
	co, c, err := openBundleCheckout(e, "push to GitHub and run `rp dev brain publish --sha <commit>`")
	if err != nil {
		return err
	}
	// Decided on the RESOLVED tenant (flag, local override or login): a tenant checkout only ever
	// publishes its own overlay, never the shared project brain's channels.
	if channel != "" && co.tenant != "" {
		return fmt.Errorf("this is tenant %s's brain: --channel promotes the shared project brain, which a tenant checkout never publishes; drop --channel to publish the tenant brain", co.tenant)
	}
	if remove && !isBundleCheckout(e, co) {
		return fmt.Errorf("--rm deletes only a checkout made by `rp dev brain clone`; %s is not one (its origin is not the bundle in .git), so it was not touched", co.dir)
	}
	if err := commitPending(e, co, message); err != nil {
		return err
	}
	var up bundlePush
	var syncResp *client.BrainSyncResponse
	var syncRaw json.RawMessage
	rebased := false
	for attempt := 1; ; attempt++ {
		if up, err = pushBundle(e, c, co, ""); err != nil {
			return err
		}
		ref := up.Ref
		if ref == "" {
			ref = up.SHA // already on main: sync answers "main already contains"
		}
		syncResp, syncRaw, err = c.BrainSyncRef(e.ctx(), co.project, co.tenant, ref)
		var apiErr *client.APIError
		if !asAPIError(err, &apiErr) || apiErr.Code != brainNotFastForward {
			break
		}
		if attempt == maxPublishAttempts {
			return withHint(fmt.Errorf("the brain's main kept moving: gave up after %d attempts (%v)", attempt, err),
				"publish again in a moment; nothing was landed")
		}
		if err := replayOntoMovedMain(e, c, co); err != nil {
			return err
		}
		rebased = true
	}
	if err != nil {
		return brainBootCheckError(err)
	}
	if syncResp.Sync.ManualReconcile {
		return fmt.Errorf("the brain on the server is %q and needs a manual reconcile: %s", syncResp.Sync.After.State, syncResp.Sync.Message)
	}
	served := strings.ToLower(syncResp.Sync.After.LocalSHA)
	if served != up.SHA && gitHasCommit(e.ctx(), co.dir, served) && gitIsAncestorIn(e.ctx(), co.dir, up.SHA, served) != nil {
		return fmt.Errorf("verify failed: the brain's main is %s, which does not contain %s", short(served), short(up.SHA))
	}
	res := localPublishResult{
		brainPublishResult: brainPublishResult{Project: syncResp.Project, Tenant: co.tenant, Channel: channel, SHA: up.SHA, OldSHA: syncResp.Sync.Before.LocalSHA,
			Sync: syncResp.Sync, Verified: true, Raw: map[string]json.RawMessage{"sync": syncRaw}},
		PreviousMain: syncResp.Sync.Before.LocalSHA, DevRef: up.Ref, Rebased: rebased, Dir: co.dir,
	}
	var statusResp *client.BrainStatusResponse
	if channel != "" {
		promoteResp, promoteRaw, err := c.BrainPromote(e.ctx(), co.project, client.BrainPromoteRequest{Channel: channel, SHA: up.SHA})
		if err != nil {
			return brainAliasError(err, co.project, "")
		}
		var statusRaw json.RawMessage
		statusResp, statusRaw, err = c.BrainStatus(e.ctx(), co.project, "")
		if err != nil {
			return err
		}
		ch := findBrainChannel(statusResp.Status.Channels, channel)
		if ch == nil || ch.ResolvedSHA != up.SHA {
			got := ""
			if ch != nil {
				got = ch.ResolvedSHA
			}
			return fmt.Errorf("verify failed: channel %q resolved %s, want %s", channel, dashOr(got), up.SHA)
		}
		res.OldSHA = promoteResp.OldSHA
		res.Raw["promote"], res.Raw["status"] = promoteRaw, statusRaw
	}
	// The land is done; from here a failure only keeps the checkout (warned on stderr), never fails publish.
	keepWhy := ""
	if _, _, _, _, err := pullBundle(e, c, co); err != nil {
		keepWhy = "refreshing it failed: " + err.Error()
	}
	if remove && keepWhy == "" {
		keepWhy = removableCheckout(e, co, up.SHA)
		if keepWhy == "" {
			if err := os.RemoveAll(co.dir); err != nil {
				keepWhy = err.Error()
			} else {
				res.Removed = true
			}
		}
	}
	switch {
	case keepWhy != "" && remove:
		_, _ = fmt.Fprintf(e.err, "Published, but kept the checkout %s: %s\n", co.dir, keepWhy)
	case keepWhy != "":
		_, _ = fmt.Fprintf(e.err, "warning: published, but %s\n", keepWhy)
	}
	if e.jsonOut() {
		raw, err := json.Marshal(res)
		if err != nil {
			return err
		}
		return e.renderJSONInline(raw)
	}
	if statusResp != nil {
		render.BrainStatus(e.out, statusResp)
	} else {
		render.BrainSync(e.out, syncResp)
		if co.tenant == "" && len(syncResp.Sync.After.Channels) > 0 {
			_, _ = fmt.Fprintf(e.out, "Tenants on a channel still serve the old commit: `rp dev brain publish --channel stable` (or edge) promotes %s.\n", short(up.SHA))
		}
	}
	if rebased {
		_, _ = fmt.Fprintf(e.out, "The brain's main had moved: your commits were replayed onto it (no overlapping paths) as %s; a proof run before this publish tested the old base.\n", short(up.SHA))
	}
	if res.Removed {
		_, _ = fmt.Fprintf(e.out, "Removed the checkout %s.\n", co.dir)
	}
	return nil
}

const (
	brainNotFastForward = "BRAIN_NOT_FAST_FORWARD"
	maxPublishAttempts  = 3
)

// localPublishResult is publish's receipt for a checkout; the --sha paths keep brainPublishResult as is.
type localPublishResult struct {
	brainPublishResult
	PreviousMain string `json:"previous_main"`
	DevRef       string `json:"dev_ref"`
	Rebased      bool   `json:"rebased"`
	Removed      bool   `json:"removed"`
	Dir          string `json:"dir"`
}

// replayOntoMovedMain rebases the current branch's commits (since the old origin/main) onto the brain's
// fresh main, but only when the paths changed upstream and locally are disjoint. On overlap or a rebase
// conflict the checkout stays exactly as it was (the rebase is aborted); only origin/* has moved.
func replayOntoMovedMain(e *env, c *client.Client, co bundleCheckout) error {
	oldBase, err := gitIn(e.ctx(), co.dir, "rev-parse", "refs/remotes/origin/main")
	if err != nil {
		return err
	}
	newBase, err := fetchBundle(e, c, co)
	if err != nil {
		return err
	}
	upstream, err := gitIn(e.ctx(), co.dir, "diff", "--name-only", "--no-renames", oldBase, newBase)
	if err != nil {
		return err
	}
	local, err := gitIn(e.ctx(), co.dir, "diff", "--name-only", "--no-renames", oldBase, "HEAD")
	if err != nil {
		return err
	}
	theirs := map[string]bool{}
	for _, p := range strings.Fields(upstream) {
		theirs[p] = true
	}
	var overlap []string
	for _, p := range strings.Split(local, "\n") {
		if p != "" && theirs[p] {
			overlap = append(overlap, p)
		}
	}
	if len(overlap) > 0 {
		return publishConflict(fmt.Sprintf("the brain's main moved and changed the same paths as your commits: %s", strings.Join(overlap, ", ")))
	}
	if _, err := gitIn(e.ctx(), co.dir, "rebase", "--quiet", "--onto", newBase, oldBase); err != nil {
		_, _ = gitIn(e.ctx(), co.dir, "rebase", "--abort")
		return publishConflict(fmt.Sprintf("the brain's main moved and replaying your commits onto it conflicted (%v)", err))
	}
	return nil
}

func publishConflict(msg string) error {
	return withHint(&commandError{code: exitUsage, name: "BRAIN_MAIN_MOVED_OVERLAP", message: msg + "; nothing was published and your checkout is unchanged"},
		"re-apply your change on a fresh main: `rp dev brain clone <project> [--tenant <slug>] --temp`, redo the edit, publish")
}

// isBundleCheckout: the checkout's origin is the bundle file in its own .git, as `rp dev brain clone`
// leaves it. A GitHub clone (or anything else) fails this, so --rm can never delete one.
func isBundleCheckout(e *env, co bundleCheckout) bool {
	url, err := gitIn(e.ctx(), co.dir, "remote", "get-url", "origin")
	return err == nil && filepath.Clean(url) == filepath.Join(co.gitDir, bundleFileName)
}

// removableCheckout is why the checkout must be kept ("" = safe to delete): the refreshed origin/main
// (the box's main) must contain sha, and nothing local may be uncommitted, stashed or unpublished.
func removableCheckout(e *env, co bundleCheckout, sha string) string {
	if gitIsAncestorIn(e.ctx(), co.dir, sha, "refs/remotes/origin/main") != nil {
		return "the brain's main as downloaded does not contain " + short(sha)
	}
	if status, err := gitIn(e.ctx(), co.dir, "status", "--porcelain"); err != nil || status != "" {
		return "it has uncommitted changes"
	}
	if _, err := gitIn(e.ctx(), co.dir, "rev-parse", "--verify", "--quiet", "refs/stash"); err == nil {
		return "it has a stash"
	}
	heads, err := gitIn(e.ctx(), co.dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return err.Error()
	}
	for _, h := range strings.Fields(heads) {
		if gitIsAncestorIn(e.ctx(), co.dir, "refs/heads/"+h, "refs/remotes/origin/main") != nil {
			return "branch " + h + " has commits the brain's main does not"
		}
	}
	return ""
}

func downloadBundle(e *env, c *client.Client, project, tenant, dst string) error {
	tmp := dst + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	derr := c.DownloadBrainBundle(e.ctx(), project, tenant, f)
	if cerr := f.Close(); derr == nil {
		derr = cerr
	}
	if derr != nil {
		_ = os.Remove(tmp)
		return derr
	}
	return os.Rename(tmp, dst)
}

// writeCheckoutMarkers binds a fresh clone to its scope: the committed .replypen.toml (when the brain
// carries none) and, for a tenant brain, the gitignored local tenant override. Returns the marker it wrote.
// The clone's tree comes from a bundle, so a marker or `.replypen` may be a committed symlink: refused,
// never written through.
func writeCheckoutMarkers(dir, gitDir, project, tenant string) (string, error) {
	for _, name := range []string{config.MarkerFileName, config.LegacyMarkerFileName} {
		if fi, err := os.Lstat(filepath.Join(dir, name)); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("the brain commits %s as a symlink; refusing to use it (replace it with a plain file)", name)
		}
	}
	wrote := ""
	if b, err := config.DiscoverBrain(dir); err != nil {
		return "", err
	} else if b == nil || b.Dir != dir {
		if err := writeInCheckout(dir, config.MarkerFileName, []byte(fmt.Sprintf("project = %q\n", project))); err != nil {
			return "", err
		}
		wrote = config.MarkerFileName
	}
	if tenant == "" {
		return wrote, nil
	}
	if err := writeInCheckout(dir, config.LocalFileName, []byte(fmt.Sprintf("tenant = %q\n", tenant))); err != nil {
		return "", err
	}
	// .git is git's own, never bundle content.
	exclude := filepath.Join(gitDir, "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		return "", err
	}
	ex, err := os.OpenFile(exclude, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	_, werr := ex.WriteString("/.replypen/\n")
	if cerr := ex.Close(); werr == nil {
		werr = cerr
	}
	return wrote, werr
}

// writeInCheckout writes root/rel, creating missing directories, but refuses when rel or any directory on
// the way is a symlink, and opens the file no-follow: a symlink committed in the brain must never make
// the CLI write outside the checkout.
func writeInCheckout(root, rel string, data []byte) error {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	cur := root
	for i, part := range parts {
		cur = filepath.Join(cur, part)
		last := i == len(parts)-1
		fi, err := os.Lstat(cur)
		switch {
		case err == nil && fi.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("%s in the checkout is a symlink; refusing to write through it", strings.Join(parts[:i+1], "/"))
		case err == nil && !last && !fi.IsDir(), err == nil && last && !fi.Mode().IsRegular():
			return fmt.Errorf("%s in the checkout is not a plain file or directory", strings.Join(parts[:i+1], "/"))
		case os.IsNotExist(err) && !last:
			if err := os.Mkdir(cur, 0o755); err != nil {
				return err
			}
		case err != nil && !os.IsNotExist(err):
			return err
		}
	}
	f, err := os.OpenFile(cur, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|openNoFollow, 0o644)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}

// assertCheckoutScope refuses to upload when the checkout names a different brain than the command
// targets (a --project/--tenant/--scope override, or a login bound to another tenant): its commits would
// land in the wrong brain.
func assertCheckoutScope(co bundleCheckout) error {
	b, err := config.DiscoverBrain(co.dir)
	if err != nil {
		return err
	}
	if b == nil || b.Dir != co.dir {
		return fmt.Errorf("%s has no %s: run this inside a checkout made by `rp dev brain clone`", co.dir, config.MarkerFileName)
	}
	tenant, err := config.CheckoutTenant(b)
	if err != nil {
		return err
	}
	projectOK := co.project == b.Primary.Project
	for _, also := range b.Also {
		projectOK = projectOK || co.project == also.Project
	}
	if !projectOK || tenant != co.tenant {
		return fmt.Errorf("this checkout is the %s brain but the command targets the %s brain; run it without --project/--tenant/--scope from the matching checkout",
			brainScopeLabel(b.Primary.Project, tenant), brainScopeLabel(co.project, co.tenant))
	}
	return nil
}

func brainScopeLabel(project, tenant string) string {
	if tenant != "" {
		return project + " tenant " + tenant
	}
	return project
}

// gitIn runs git in dir ("" = the current directory) with hooks disabled, returning trimmed stdout.
func gitIn(ctx context.Context, dir string, args ...string) (string, error) {
	full := []string{"-c", "core.hooksPath=/dev/null"}
	if dir != "" {
		full = append(full, "-C", dir)
	}
	cmd := exec.CommandContext(ctx, "git", append(full, args...)...)
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

func currentBranch(ctx context.Context, dir string) string {
	b, err := gitIn(ctx, dir, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return b
}

func gitHasCommit(ctx context.Context, dir, sha string) bool {
	if sha == "" {
		return false
	}
	_, err := gitIn(ctx, dir, "cat-file", "-e", sha+"^{commit}")
	return err == nil
}

// gitIsAncestorIn: nil ⇒ ancestor is reachable from descendant in dir.
func gitIsAncestorIn(ctx context.Context, dir, ancestor, descendant string) error {
	_, err := gitIn(ctx, dir, "merge-base", "--is-ancestor", ancestor, descendant)
	return err
}

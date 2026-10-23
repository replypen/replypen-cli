package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os/exec"
	"path"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/render"
)

func newMirrorCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "mirror", Short: "Refresh project or tenant source mirrors"}
	cmd.AddCommand(mirrorRefreshCmd(e), mirrorPublishCmd(e))
	return cmd
}

// mirrorTipRetries bounds how often --accept-descendant chases a tip that keeps moving (each retry is
// a full host sweep), so a busy branch cannot loop forever.
const mirrorTipRetries = 3

func mirrorRefreshCmd(e *env) *cobra.Command {
	var repo, expectedSHA string
	var acceptDescendant bool
	cmd := &cobra.Command{
		Use:   "refresh --repo <name> --expect-sha <commit> [--accept-descendant]",
		Short: "Refresh mirrors and verify one repository at an exact commit (--tenant for a tenant's own mirror)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			repo = strings.TrimSpace(repo)
			if repo == "" {
				return fmt.Errorf("--repo is required")
			}
			if !fullGitSHA.MatchString(expectedSHA) {
				return fmt.Errorf("--expect-sha must be an exact full 40-character commit SHA")
			}
			expected := strings.ToLower(expectedSHA)
			c, err := e.newClient()
			if err != nil {
				return err
			}
			if err := e.resolvePinnedProject(c); err != nil {
				return err
			}
			refresh := func(sha string) (*client.MirrorRefreshResponse, []byte, error) {
				return c.MirrorRefresh(e.ctx(), e.scopeProject(), e.scopeTenant(), client.MirrorRefreshRequest{Repo: repo, ExpectedSHA: sha})
			}
			resp, raw, err := refresh(expected)
			for attempt := 0; err != nil && attempt < mirrorTipRetries; attempt++ {
				actual, ok := mirrorMismatchActual(err)
				if !ok {
					return err
				}
				if !acceptDescendant {
					return withHint(err, "the mirror tip moved; if your commit is an ancestor of actual_sha, re-run with --accept-descendant")
				}
				if aerr := gitIsAncestor(e.ctx(), expected, actual); aerr != nil {
					return withHint(err, fmt.Sprintf("--accept-descendant: %s is not verified as an ancestor of %s (%v); pull/rebase onto the new tip, push, and refresh at the SHA you pushed", short(expected), short(actual), aerr))
				}
				resp, raw, err = refresh(actual)
			}
			if err != nil {
				return err
			}
			moved := resp.ActualSHA != expected
			if e.jsonOut() {
				if moved {
					_, _ = fmt.Fprintf(e.err, "mirror tip moved: %s → %s (descendant), verified at %s\n", short(expected), short(resp.ActualSHA), short(resp.ActualSHA))
				}
				return e.renderJSONInline(raw)
			}
			if moved {
				_, _ = fmt.Fprintf(e.out, "Tip moved:  %s → %s (your commit is an ancestor)\n", expected, resp.ActualSHA)
			}
			render.MirrorRefresh(e.out, resp)
			return nil
		},
	}
	cmd.Flags().StringVar(&repo, "repo", "", "configured mirror name")
	cmd.Flags().StringVar(&expectedSHA, "expect-sha", "", "exact full 40-character commit SHA expected after refresh")
	cmd.Flags().BoolVar(&acceptDescendant, "accept-descendant", false, "also accept a newer tip that contains --expect-sha (ancestry checked with git in the current directory)")
	_ = cmd.MarkFlagRequired("repo")
	_ = cmd.MarkFlagRequired("expect-sha")
	return cmd
}

// mirrorMismatchActual returns the host's actual_sha from a 409 MIRROR_SHA_MISMATCH.
func mirrorMismatchActual(err error) (string, bool) {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "MIRROR_SHA_MISMATCH" {
		return "", false
	}
	for _, f := range apiErr.Fields {
		if f.Key == "actual_sha" && fullGitSHA.MatchString(f.Message) {
			return strings.ToLower(f.Message), true
		}
	}
	return "", false
}

// gitIsAncestor proves ancestor is reachable from descendant using git in the current directory,
// fetching descendant from origin first when it is not present locally. nil ⇒ proven ancestor.
func gitIsAncestor(ctx context.Context, ancestor, descendant string) error {
	git := func(args ...string) error {
		out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput()
		if err != nil && len(out) > 0 {
			return fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(out)))
		}
		return err
	}
	if err := git("cat-file", "-e", descendant+"^{commit}"); err != nil {
		if ferr := git("fetch", "--quiet", "origin", descendant); ferr != nil {
			return ferr
		}
	}
	if err := git("merge-base", "--is-ancestor", ancestor, descendant); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return errors.New("not an ancestor")
		}
		return err
	}
	return nil
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// mirrorConsumer is one project whose mirror named repo tracks branch; tip is origin/<branch> as
// fetched in the current directory, the exact commit that mirror must reach.
type mirrorConsumer struct {
	Project string `json:"project"`
	Branch  string `json:"branch"`
	Tip     string `json:"expected_sha"`
}

type mirrorPublishResult struct {
	Repo      string                          `json:"repo"`
	SHA       string                          `json:"sha"`
	Refreshed []*client.MirrorRefreshResponse `json:"refreshed"`
	Failed    map[string]string               `json:"failed,omitempty"`
	// Raw carries each refresh body verbatim as "refresh:<project>" (SKILL.md "Output" rule b).
	Raw map[string]json.RawMessage `json:"raw"`
}

// mirrorPublishCmd is "publish" for a source repo that projects mirror: one command from its checkout
// refreshes every mirror of it the login can see, each at the tip of the branch it tracks, and proves
// that tip contains --sha. It never pushes: which branch a commit is promoted to is the repo's policy.
func mirrorPublishCmd(e *env) *cobra.Command {
	var repo, sha string
	cmd := &cobra.Command{
		Use:   "publish --sha <commit> [--repo <name>]",
		Short: "Refresh every project mirroring this repository and verify each contains a pushed commit",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if !fullGitSHA.MatchString(sha) {
				return fmt.Errorf("--sha must be an exact full 40-character commit SHA")
			}
			sha = strings.ToLower(sha)
			origin, err := gitOutput(e.ctx(), "remote", "get-url", "origin")
			if err != nil {
				return withHint(err, "run from the source repository's checkout")
			}
			if repo = strings.TrimSpace(repo); repo == "" {
				repo = strings.TrimSuffix(path.Base(strings.TrimSuffix(origin, "/")), ".git")
			}
			c, err := e.newClient()
			if err != nil {
				return err
			}
			projects := []string{e.project}
			if e.project == "" {
				resp, _, err := c.Projects(e.ctx())
				if err != nil {
					return err
				}
				projects = projects[:0]
				for _, p := range resp.Projects {
					projects = append(projects, p.Name)
				}
			}
			consumers, err := mirrorConsumers(e, c, projects, repo, repoIdentity(origin))
			if err != nil {
				return err
			}
			if len(consumers) == 0 {
				return fmt.Errorf("no visible project mirrors %q from %s (rp project repo ls); pass --repo with the configured mirror name", repo, origin)
			}
			var missing []string
			tips := map[string]string{}
			for i := range consumers {
				b := consumers[i].Branch
				if _, ok := tips[b]; !ok {
					if _, err := gitOutput(e.ctx(), "fetch", "--quiet", "origin", "+refs/heads/"+b+":refs/remotes/origin/"+b); err != nil {
						return err
					}
					tip, err := gitOutput(e.ctx(), "rev-parse", "refs/remotes/origin/"+b)
					if err != nil {
						return err
					}
					tips[b] = strings.ToLower(tip)
					if tips[b] != sha && gitIsAncestor(e.ctx(), sha, tips[b]) != nil {
						missing = append(missing, b)
					}
				}
				consumers[i].Tip = tips[b]
			}
			if len(missing) > 0 {
				sort.Strings(missing)
				return withHint(fmt.Errorf("origin branch(es) %s do not contain %s; nothing refreshed", strings.Join(missing, ", "), short(sha)),
					fmt.Sprintf("push it first, e.g. `git push origin %s:%s`", sha, missing[0]))
			}
			res := mirrorPublishResult{Repo: repo, SHA: sha, Failed: map[string]string{}, Raw: map[string]json.RawMessage{}}
			for _, m := range consumers {
				out, raw, err := c.MirrorRefresh(e.ctx(), m.Project, "", client.MirrorRefreshRequest{Repo: repo, ExpectedSHA: m.Tip})
				if actual, ok := mirrorMismatchActual(err); ok && gitIsAncestor(e.ctx(), m.Tip, actual) == nil {
					out, raw, err = c.MirrorRefresh(e.ctx(), m.Project, "", client.MirrorRefreshRequest{Repo: repo, ExpectedSHA: actual})
				}
				if err != nil {
					res.Failed[m.Project] = err.Error()
					continue
				}
				res.Refreshed = append(res.Refreshed, out)
				res.Raw["refresh:"+m.Project] = raw
			}
			if e.jsonOut() {
				raw, err := json.Marshal(res)
				if err != nil {
					return err
				}
				if err := e.renderJSONInline(raw); err != nil {
					return err
				}
			} else {
				for _, r := range res.Refreshed {
					_, _ = fmt.Fprintf(e.out, "LIVE  %-24s %-10s %s\n", r.Project, r.Branch, r.ActualSHA)
				}
			}
			if len(res.Failed) > 0 {
				names := make([]string, 0, len(res.Failed))
				for p, msg := range res.Failed {
					names = append(names, p+": "+msg)
				}
				sort.Strings(names)
				return fmt.Errorf("mirror refresh failed for %d of %d project(s):\n  %s", len(res.Failed), len(consumers), strings.Join(names, "\n  "))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&sha, "sha", "", "exact full 40-character commit SHA, already pushed to every tracked branch")
	cmd.Flags().StringVar(&repo, "repo", "", "configured mirror name (default: this checkout's origin repository name)")
	_ = cmd.MarkFlagRequired("sha")
	return cmd
}

// mirrorConsumers lists every (project, tracked branch) whose mirror is named repo AND clones this
// checkout's origin: a same-named mirror of another repository must never be refreshed.
func mirrorConsumers(e *env, c *client.Client, projects []string, repo, origin string) ([]mirrorConsumer, error) {
	var out []mirrorConsumer
	for _, p := range projects {
		list, _, err := c.List(e.ctx(), "repos", p, "")
		var apiErr *client.APIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden {
			_, _ = fmt.Fprintf(e.err, "skipped %s: this login cannot read its mirrors\n", p)
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: list mirrors: %w", p, err)
		}
		for _, item := range list.Items {
			var name, branch, gitURL string
			_ = json.Unmarshal(item["name"], &name)
			_ = json.Unmarshal(item["default_branch"], &branch)
			_ = json.Unmarshal(item["git_url"], &gitURL)
			if name != repo || repoIdentity(gitURL) != origin {
				continue
			}
			if branch == "" {
				branch = "main"
			}
			out = append(out, mirrorConsumer{Project: p, Branch: branch})
		}
	}
	return out, nil
}

func gitOutput(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(exitErr.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

// repoIdentity normalizes a clone URL to host/owner/name so https, ssh and scp-style remotes of one
// repository compare equal.
func repoIdentity(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	for _, prefix := range []string{"https://", "http://", "ssh://", "git://"} {
		s = strings.TrimPrefix(s, prefix)
	}
	if at := strings.Index(s, "@"); at >= 0 && at < strings.IndexAny(s+"/", "/:") {
		s = s[at+1:]
	}
	s = strings.Replace(s, ":", "/", 1)
	return strings.TrimSuffix(strings.TrimSuffix(s, "/"), ".git")
}

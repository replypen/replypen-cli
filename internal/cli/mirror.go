package cli

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/render"
)

func newMirrorCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "mirror", Short: "Refresh project or tenant source mirrors"}
	cmd.AddCommand(mirrorRefreshCmd(e))
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

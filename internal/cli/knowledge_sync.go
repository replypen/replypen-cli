package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/render"
)

// knowledgeSyncPollInterval paces --wait; tests shorten it.
var knowledgeSyncPollInterval = 2 * time.Second

// newKnowledgeSyncRunCmd queues the scope's KB sync now instead of waiting for the periodic sweep (daily;
// bundle sources hourly), e.g. right after a help article went live.
func newKnowledgeSyncRunCmd(e *env) *cobra.Command {
	var wait bool
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "run [--wait]",
		Short: "Sync the knowledge base now (e.g. after publishing a help article)",
		Long: "Queue a sync of this project's knowledge sources into /kb now, instead of waiting for the\n" +
			"periodic sweep (daily; site-published bundles hourly). The project route syncs the project KB and\n" +
			"every tenant's; --tenant syncs only that tenant's own KB. A sync already queued absorbs the request;\n" +
			"one already running gets a single follow-up sync, so a change made just now is always picked up.\n\n" +
			"--wait polls until the sync finishes and prints the article total and the article files that\n" +
			"changed; it exits non-zero when the sync failed or the knowledge source could not be read.\n" +
			"Needs the `knowledge:write` (or config:write) token scope.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := e.newClient()
			if err != nil {
				return err
			}
			if err := e.resolvePinnedProject(c); err != nil {
				return err
			}
			queued, raw, err := c.KnowledgeSyncNow(e.ctx(), e.scopeProject(), e.scopeTenant())
			if err != nil {
				return err
			}
			if !wait {
				if e.jsonOut() {
					return e.renderJSONInline(raw)
				}
				verb := "queued"
				if queued.Deduped {
					verb = "already queued"
				}
				_, _ = fmt.Fprintf(e.out, "kb sync job %d %s (%s)\n", queued.JobID, verb, queued.State)
				return nil
			}
			done, raw, err := waitForKnowledgeSync(e, c, queued, timeout)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				if err := e.renderJSONInline(raw); err != nil {
					return err
				}
			} else {
				printKnowledgeSync(e, done)
			}
			return knowledgeSyncFailure(done)
		},
	}
	cmd.Flags().BoolVar(&wait, "wait", false, "poll until the sync finishes and print what changed")
	cmd.Flags().DurationVar(&timeout, "timeout", 15*time.Minute, "max time to wait under --wait")
	return cmd
}

func waitForKnowledgeSync(e *env, c *client.Client, queued *client.KnowledgeSyncStatus, timeout time.Duration) (*client.KnowledgeSyncStatus, json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(e.ctx(), timeout)
	defer cancel()
	showProgress := render.IsTerminal(e.err)
	defer func() {
		if showProgress {
			_, _ = fmt.Fprintf(e.err, "\r\033[K")
		}
	}()
	last := queued.State
	for {
		timer := time.NewTimer(knowledgeSyncPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, fmt.Errorf("timed out after %s waiting for kb sync job %d (last state: %s); it keeps running on the server", timeout, queued.JobID, last)
		case <-timer.C:
		}
		st, raw, err := c.KnowledgeSyncJob(ctx, e.scopeProject(), e.scopeTenant(), queued.JobID, queued.KBCommit)
		if err != nil {
			if ctx.Err() != nil {
				return nil, nil, fmt.Errorf("timed out after %s waiting for kb sync job %d (last state: %s)", timeout, queued.JobID, last)
			}
			return nil, nil, err
		}
		if st.Done {
			return st, raw, nil
		}
		last = st.State
		if showProgress {
			_, _ = fmt.Fprintf(e.err, "\r\033[Kkb sync job %d … %s", st.JobID, st.State)
		}
	}
}

func printKnowledgeSync(e *env, st *client.KnowledgeSyncStatus) {
	_, _ = fmt.Fprintf(e.out, "kb sync job %d %s · health %s · %d articles · %d changed\n",
		st.JobID, st.State, orDash(st.HealthState), st.ArticleTotal, len(st.Changed))
	for _, f := range st.Changed {
		_, _ = fmt.Fprintf(e.out, "  %s\n", f)
	}
	if st.Error != "" {
		_, _ = fmt.Fprintf(e.out, "error: %s\n", st.Error)
	}
}

// knowledgeSyncFailure is the --wait exit contract: a job that did not complete, or a scope whose
// source could not be read (health not ok), is a failure a script must see.
func knowledgeSyncFailure(st *client.KnowledgeSyncStatus) error {
	if st.State != "completed" {
		return fmt.Errorf("kb sync job %d ended %s", st.JobID, st.State)
	}
	if st.HealthState != "" && st.HealthState != "ok" {
		return fmt.Errorf("kb sync job %d completed but the knowledge source is %s (see rp status)", st.JobID, strings.ReplaceAll(st.HealthState, "_", " "))
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

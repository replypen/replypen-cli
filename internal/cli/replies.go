package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rootcause-org/rootcause-cli/internal/render"
)

// newRepliesCmd is `rc dev replies`: a tenant's answered turns (trigger → our draft → the human's send)
// and the tenant-fenced feedback write. Needs a runs:read token; project + tenant come from the
// ambient selectors.
func newRepliesCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "replies", Short: "Review a tenant's answered mails and rate the proposed replies"}
	cmd.AddCommand(repliesListCmd(e), repliesFeedbackCmd(e))
	return cmd
}

func repliesListCmd(e *env) *cobra.Command {
	var limit int
	var cursor string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List a tenant's answered mails, newest first",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := e.newClient()
			if err != nil {
				return err
			}
			list, raw, err := c.TenantReplies(e.ctx(), e.scopeProject(), e.scopeTenant(), limit, cursor)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				return e.renderJSON("replies", raw)
			}
			render.TenantReplies(e.out, list)
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 0, "max turns per page (server default 50, cap 100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "next_cursor from a previous page")
	return cmd
}

func repliesFeedbackCmd(e *env) *cobra.Command {
	var score int
	var comment string
	cmd := &cobra.Command{
		Use:   "feedback <run-id> [--score N] [--comment TEXT]",
		Short: "Rate one proposed reply (score 1-5 and/or comment, saved independently)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cc *cobra.Command, args []string) error {
			body := map[string]any{}
			if cc.Flags().Changed("score") {
				body["score"] = score
			}
			if cc.Flags().Changed("comment") {
				body["comment"] = comment
			}
			if len(body) == 0 {
				return fmt.Errorf("nothing to record: pass --score and/or --comment")
			}
			c, err := e.newClient()
			if err != nil {
				return err
			}
			raw, err := c.TenantReplyFeedback(e.ctx(), e.scopeProject(), e.scopeTenant(), args[0], body)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				return e.renderJSONInline(raw)
			}
			_, _ = fmt.Fprintf(e.out, "feedback recorded for run %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().IntVar(&score, "score", 0, "score 1-5")
	cmd.Flags().StringVar(&comment, "comment", "", "free-text comment (an empty value clears it)")
	return cmd
}

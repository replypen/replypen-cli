package cli

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/render"
)

// newFleetValueCmd builds `rp fleet value`: per tenant/mailbox/month, drafted runs and how many saw the
// thread continue afterwards — the value-based pricing count. Server returns rows; totals and % are ours.
func newFleetValueCmd(e *env) *cobra.Command {
	var months int
	var since, until string
	cmd := &cobra.Command{
		Use:   "value",
		Short: "Drafted mails and how many got a reply sent afterwards, per tenant, mailbox and month",
		Long: "Per UTC month, tenant and mailbox: real inbound e-mail runs (simulations excluded), runs whose draft " +
			"reached the mailbox, and how many of those saw the thread continue afterwards — the draft sent as is, " +
			"edited, rewritten (≈ an own reply), or auto-sent. Thread counts sit next to run counts. Counts only.\n\n" +
			"Needs --project (or a brain checkout) and a human global-admin login. --tenant narrows to one tenant.\n\n" +
			"  rp --project kampadmin --tenant lbv fleet value --months 3\n" +
			"  rp --project kampadmin fleet value --since 2026-09-01 --until 2026-12-01 -o json",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("months") && (since != "" || until != "") {
				return fmt.Errorf("pass either --months or --since/--until, not both")
			}
			if until != "" && since == "" {
				return fmt.Errorf("--until needs --since")
			}
			c, err := e.newClient()
			if err != nil {
				return err
			}
			project := e.scopeProject()
			if project == "" {
				project = e.resolved.Project
			}
			if project == "" {
				return fmt.Errorf("rp fleet value needs --project")
			}
			q := url.Values{}
			if since != "" || until != "" {
				q.Set("since", since)
				if until != "" {
					q.Set("until", until)
				}
			} else {
				q.Set("months", strconv.Itoa(months))
			}
			if t := e.scopeTenant(); t != "" {
				q.Set("tenant", t)
			}
			stats, raw, err := c.AdminValueStats(e.ctx(), project, q)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				return e.renderJSONInline(raw)
			}
			render.ValueStats(e.out, stats)
			return nil
		},
	}
	cmd.Flags().IntVar(&months, "months", 3, "whole UTC months ending with the current one")
	cmd.Flags().StringVar(&since, "since", "", "window start YYYY-MM-DD (instead of --months)")
	cmd.Flags().StringVar(&until, "until", "", "window end YYYY-MM-DD, exclusive (default: tomorrow)")
	return cmd
}

package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/render"
)

func adminIdentityFailuresCmd(e *env) *cobra.Command {
	var simulation bool
	var cursor string
	cmd := &cobra.Command{
		Use:   "identity-failures <project>",
		Short: "List identity failures without patient details (global admin)",
		Long:  "List 50 identity failures, newest first. The project name or UUID is required. Live runs only unless --simulation is set; use --cursor for the next page.",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := e.newClient()
			if err != nil {
				return err
			}
			list, raw, err := c.AdminIdentityFailures(e.ctx(), args[0], simulation, cursor)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				return e.renderJSONInline(raw)
			}
			render.IdentityFailures(e.out, list)
			if list.NextCursor != "" {
				_, _ = fmt.Fprintf(e.err, "more: --cursor %s\n", list.NextCursor)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&simulation, "simulation", false, "show simulation failures instead of live failures")
	cmd.Flags().StringVar(&cursor, "cursor", "", "next_cursor from a previous page")
	return cmd
}

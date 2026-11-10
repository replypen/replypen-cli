package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// adminRunUsageCmd: per-run LLM cost and tokens, for old-vs-new evaluation. Box-admin only on the server:
// cost is our unit economics and never rides a customer/operator run read.
func adminRunUsageCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "run-usage <project> <run-id>...",
		Short: "Summed LLM cost and tokens per run (human global admin; evaluation only)",
		Long: "Summed LLM cost (USD) and prompt/completion/reasoning tokens per run, across every model call the run " +
			"made (loop, grounding, side steps). Up to 200 run ids. A run with no usage rows is absent. Global-admin " +
			"only: cost is internal unit economics, never shown on customer surfaces.",
		Args: cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			ids := make([]string, 0, len(args)-1)
			for _, a := range args[1:] {
				ids = append(ids, normalizeRunID(a))
			}
			c, err := e.newClient()
			if err != nil {
				return err
			}
			list, raw, err := c.AdminRunUsage(e.ctx(), args[0], ids)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				return e.renderJSONInline(raw)
			}
			_, _ = fmt.Fprintf(e.out, "%-36s %6s %10s %10s %10s %10s\n", "RUN", "CALLS", "COST_USD", "PROMPT", "COMPLETION", "TOTAL")
			for _, u := range list.Items {
				_, _ = fmt.Fprintf(e.out, "%-36s %6d %10.4f %10d %10d %10d\n", u.RunID, u.Calls, u.CostUSD, u.PromptTokens, u.CompletionTokens, u.TotalTokens)
			}
			return nil
		},
	}
}

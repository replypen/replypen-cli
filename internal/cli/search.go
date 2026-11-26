package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/digest"
	"github.com/replypen/replypen-cli/internal/render"
)

func newSearchCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "search", Short: "Read what visitors searched in the project's website help center"}
	cmd.AddCommand(newSearchReportCmd(e), newSearchEventsCmd(e))
	return cmd
}

type searchWindow struct{ from, to string }

func (w *searchWindow) flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&w.from, "from", "", "first day, YYYY-MM-DD (default: 30 days before --to)")
	cmd.Flags().StringVar(&w.to, "to", "", "day after the last, YYYY-MM-DD (default: tomorrow, UTC)")
}

func fetchSearchEvents(e *env, w searchWindow) (*client.SearchEventList, json.RawMessage, error) {
	c, err := e.newClient()
	if err != nil {
		return nil, nil, err
	}
	project := e.scopeProject()
	if project == "" {
		return nil, nil, fmt.Errorf("search requires --project or a brain project")
	}
	return c.SearchEvents(e.ctx(), project, w.from, w.to)
}

func newSearchReportCmd(e *env) *cobra.Command {
	var w searchWindow
	var limit int
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Help-center searches per query: zero results, no click, clicks by rank",
		Long: "Aggregate the project's help-center search events per distinct query (case-folded, redacted on the server): " +
			"searches, zero results, searches with results but no click, the result list most often shown, clicks per " +
			"article and rank, site revisions and ranker versions seen, first and last day. -o json emits that digest; " +
			"`rp project search events -o json` gives the raw rows.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			list, _, err := fetchSearchEvents(e, w)
			if err != nil {
				return err
			}
			rep := digest.SearchReportFrom(list)
			if e.jsonOut() {
				b, err := json.Marshal(rep)
				if err != nil {
					return err
				}
				return e.renderJSON("search-report", b)
			}
			render.SearchReport(e.out, rep, limit)
			return nil
		},
	}
	w.flags(cmd)
	cmd.Flags().IntVar(&limit, "limit", 25, "rows per list in the table (0 = all)")
	return cmd
}

func newSearchEventsCmd(e *env) *cobra.Command {
	var w searchWindow
	cmd := &cobra.Command{
		Use:   "events",
		Short: "Raw help-center search and click events, oldest first",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			list, raw, err := fetchSearchEvents(e, w)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				return e.renderJSON("search-events", raw)
			}
			_, _ = fmt.Fprintf(e.out, "%d events %s → %s (use -o json for the rows)\n", len(list.Items), list.From, list.To)
			return nil
		},
	}
	w.flags(cmd)
	return cmd
}

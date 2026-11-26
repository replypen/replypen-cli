package render

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/replypen/replypen-cli/internal/digest"
)

// SearchReport prints the per-query help-search digest: one row per query, plus the three miss lists the
// weekly tune-up works from. -o json carries the full digest (shown lists, clicks per rank, revs).
func SearchReport(w io.Writer, r digest.SearchReport, limit int) {
	_, _ = fmt.Fprintf(w, "Search report %s  %s → %s  (%d searches, %d clicks, %d distinct queries)\n",
		r.Project, r.From, r.To, r.Searches, r.Clicks, len(r.Queries))
	if r.Truncated {
		_, _ = fmt.Fprintln(w, "TRUNCATED: the window held more events than one read returns; narrow --from/--to.")
	}
	if len(r.Queries) == 0 {
		_, _ = fmt.Fprintln(w, "No searches in this window.")
		return
	}
	section := func(title string, keep func(digest.SearchQuery) bool, by func(digest.SearchQuery) int) {
		var rows []digest.SearchQuery
		for _, q := range r.Queries {
			if keep(q) {
				rows = append(rows, q)
			}
		}
		sort.SliceStable(rows, func(i, j int) bool { return by(rows[i]) > by(rows[j]) })
		_, _ = fmt.Fprintf(w, "\n## %s\n", title)
		if len(rows) == 0 {
			_, _ = fmt.Fprintln(w, "(none)")
			return
		}
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "SEARCHES\tZERO\tNO CLICK\tCLICKS (id@rank)\tQUERY")
		for i, q := range rows {
			if limit > 0 && i == limit {
				break
			}
			_, _ = fmt.Fprintf(tw, "%d\t%d\t%d\t%s\t%s\n", q.Searches, q.ZeroResults, q.NoClick, clickCell(q), q.Q)
		}
		_ = tw.Flush()
	}
	all := func(digest.SearchQuery) bool { return true }
	section("Top queries", all, func(q digest.SearchQuery) int { return q.Searches })
	section("Zero results", func(q digest.SearchQuery) bool { return q.ZeroResults > 0 }, func(q digest.SearchQuery) int { return q.ZeroResults })
	section("Results but no click", func(q digest.SearchQuery) bool { return q.NoClick > 0 }, func(q digest.SearchQuery) int { return q.NoClick })
	section("Clicked at rank 3 or lower", digest.SearchQuery.LowRank, func(q digest.SearchQuery) int { return q.Searches })
}

func clickCell(q digest.SearchQuery) string {
	if len(q.Clicks) == 0 {
		return "-"
	}
	var parts []string
	for _, c := range q.Clicks {
		ranks := make([]string, 0, len(c.Ranks))
		for r := range c.Ranks {
			ranks = append(ranks, r)
		}
		sort.Strings(ranks)
		parts = append(parts, fmt.Sprintf("%s@%s×%d", c.ID, strings.Join(ranks, "/"), c.Clicks))
	}
	return strings.Join(parts, ", ")
}

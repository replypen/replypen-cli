package render

import (
	"fmt"
	"io"
	"strconv"
	"text/tabwriter"

	"github.com/replypen/replypen-cli/internal/client"
)

// ValueStats renders `rp fleet value`: one line per month×tenant×mailbox, per-tenant totals, a grand total.
func ValueStats(w io.Writer, s *client.ValueStats) {
	_, _ = fmt.Fprintf(w, "%s %s → %s (UTC, until exclusive)\n\n", s.Project, s.Since, s.Until)
	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', tabwriter.AlignRight)
	_, _ = fmt.Fprintln(tw, "MONTH\tTENANT\tMAILBOX\tCHANNEL\tRUNS\tDRAFTED\tCONTINUED\t%\tVERBATIM\tMINOR\tMAJOR\tREWRITTEN\tAUTO\tTHREADS\tCONT.THR\t%\t")
	line := func(month, tenant, mailbox, channel string, r client.ValueStatsRow) {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%d\t%d\t%s\t%d\t%d\t%d\t%d\t%d\t%d\t%d\t%s\t\n", month, tenant, mailbox, channel,
			r.Runs, r.DraftedRuns, r.ContinuedRuns, valuePct(r.ContinuedRuns, r.DraftedRuns),
			r.SentVerbatim, r.SentMinorEdit, r.SentMajorEdit, r.SentRewritten, r.AutoSent,
			r.DraftedThreads, r.ContinuedThreads, valuePct(r.ContinuedThreads, r.DraftedThreads))
	}
	var total client.ValueStatsRow
	byTenant := map[string]*client.ValueStatsRow{}
	var tenants []string
	for _, r := range s.Items {
		line(r.Month, valueDash(r.Tenant), valueDash(r.Mailbox), valueDash(r.Provider), r)
		addValueRow(&total, r)
		if byTenant[r.Tenant] == nil {
			byTenant[r.Tenant] = &client.ValueStatsRow{}
			tenants = append(tenants, r.Tenant)
		}
		addValueRow(byTenant[r.Tenant], r)
	}
	if len(tenants) > 1 {
		for _, t := range tenants {
			line("total", valueDash(t), "", "", *byTenant[t])
		}
	}
	line("TOTAL", "", "", "", total)
	_ = tw.Flush()
	_, _ = fmt.Fprintln(w, "\nVERBATIM..REWRITTEN split the human sends by text similarity to our draft (REWRITTEN ≈ own reply; older captures carry none, so the split may not add up to CONTINUED).")
	_, _ = fmt.Fprintln(w, "Thread totals add up per month; a thread drafted in two months counts in both.")
}

func addValueRow(dst *client.ValueStatsRow, r client.ValueStatsRow) {
	dst.Runs += r.Runs
	dst.DraftedRuns += r.DraftedRuns
	dst.ContinuedRuns += r.ContinuedRuns
	dst.SentVerbatim += r.SentVerbatim
	dst.SentMinorEdit += r.SentMinorEdit
	dst.SentMajorEdit += r.SentMajorEdit
	dst.SentRewritten += r.SentRewritten
	dst.AutoSent += r.AutoSent
	dst.DraftedThreads += r.DraftedThreads
	dst.ContinuedThreads += r.ContinuedThreads
}

func valuePct(n, d int64) string {
	if d == 0 {
		return "-"
	}
	return strconv.FormatInt((n*100+d/2)/d, 10) + "%"
}

func valueDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"
	_ "time/tzdata" // Brussels zone even where the OS ships no zoneinfo

	"github.com/replypen/replypen-cli/internal/client"
)

// TenantBudget renders the project-admin view of one tenant's monthly AI limit.
func TenantBudget(w io.Writer, tenant string, l *client.TenantBudgetLimit) {
	resets := budgetResetDate(l.ResetsAt)
	_, _ = fmt.Fprintf(w, "tenant     %s\n", tenant)
	if !l.Metered || l.MonthlyUSD == nil {
		_, _ = fmt.Fprintln(w, "limit      none (create one: rp project tenant budget set <slug> --usd <amount>)")
		return
	}
	_, _ = fmt.Fprintf(w, "account    %s\n", l.Account)
	_, _ = fmt.Fprintf(w, "limit      $%.2f per month (recurring)\n", *l.MonthlyUSD)
	if l.UsedPercent != nil {
		_, _ = fmt.Fprintf(w, "used       %d%% this month, resets %s\n", *l.UsedPercent, resets)
	}
	paused := "no"
	if l.Paused {
		paused = "yes: no new AI work until the limit is raised or the month resets"
	}
	_, _ = fmt.Fprintf(w, "paused     %s\n", paused)
	_, _ = fmt.Fprintf(w, "shared by  %s\n", strings.Join(l.Tenants, ", "))
}

// TenantBudgets renders every tenant's limit in one project.
func TenantBudgets(w io.Writer, b *client.ProjectBudgets) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "TENANT\tLIMIT/MONTH\tUSED\tPAUSED\tSHARED BY")
	for _, r := range b.Tenants {
		if !r.Metered || r.MonthlyUSD == nil {
			_, _ = fmt.Fprintf(tw, "%s\tnone\t-\t-\t-\n", r.Tenant)
			continue
		}
		used, paused := "-", "no"
		if r.UsedPercent != nil {
			used = fmt.Sprintf("%d%%", *r.UsedPercent)
		}
		if r.Paused {
			paused = "yes"
		}
		_, _ = fmt.Fprintf(tw, "%s\t$%.2f\t%s\t%s\t%s\n", r.Tenant, *r.MonthlyUSD, used, paused, strings.Join(r.Tenants, ", "))
	}
	_ = tw.Flush()
}

func budgetResetDate(raw string) string {
	resets := raw
	// The budget month is Europe/Brussels; in UTC the reset instant falls on the previous day.
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		if loc, err := time.LoadLocation("Europe/Brussels"); err == nil {
			t = t.In(loc)
		}
		resets = t.Format("2 Jan 2006")
	}
	return resets
}

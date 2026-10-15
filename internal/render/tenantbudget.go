package render

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/rootcause-org/rootcause-cli/internal/client"
)

// TenantBudget renders the project-admin view of one tenant's monthly AI limit.
func TenantBudget(w io.Writer, tenant string, l *client.TenantBudgetLimit) {
	resets := l.ResetsAt
	if t, err := time.Parse(time.RFC3339Nano, l.ResetsAt); err == nil {
		resets = t.Format("2 Jan 2006")
	}
	_, _ = fmt.Fprintf(w, "tenant     %s\n", tenant)
	if !l.Metered || l.MonthlyUSD == nil {
		_, _ = fmt.Fprintln(w, "limit      none (unmetered: ask ReplyPen support to set one up)")
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

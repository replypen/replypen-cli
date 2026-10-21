package render

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/replypen/replypen-cli/internal/client"
)

func PhoneLabRuns(w io.Writer, resp *client.PhoneLabList, project string) {
	if len(resp.Items) == 0 {
		_, _ = fmt.Fprintln(w, "No phone-lab calls.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "RUN\tSTATUS\tTENANT ID\tSTARTED")
	for _, r := range resp.Items {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", r.RunID, r.Status, r.TenantID, r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintf(w, "\nDrill: rp run debug <run_id> --phone-lab --project %s\n", project)
}

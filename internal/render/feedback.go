package render

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/replypen/replypen-cli/internal/client"
)

// Feedback renders the rated-runs table of `rp run feedback list`, newest rating first. The full run id
// leads so `rp run show <run_id>` can be copy-pasted; comment and answer are clipped (-o json has them whole).
func Feedback(w io.Writer, resp *client.FeedbackResponse) {
	if len(resp.Feedback) == 0 {
		_, _ = fmt.Fprintln(w, "No feedback.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "RUN\tKIND\tTENANT\tSCORE\tCOMMENT\tANSWER\tWHEN")
	for _, f := range resp.Feedback {
		score := "-"
		if f.Score > 0 {
			score = fmt.Sprintf("%d★", f.Score)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			f.RunID, f.Kind, dash(f.TenantSlug), score,
			orDashTrimmed(truncate(f.Comment, 40), "-"), orDashTrimmed(truncate(f.AnswerPreview, 50), "-"), f.FeedbackAt)
	}
	_ = tw.Flush()
	if resp.NextBefore != "" {
		// The hint can't know the caller's filters or scope; dropping them would change the next page.
		_, _ = fmt.Fprintf(w, "\nMore: rerun the same command with --before %s\n", resp.NextBefore)
	}
}

package render

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/rootcause-org/rootcause-cli/internal/client"
)

// TenantReplies renders `rc dev replies list`: one line per answered turn, newest first, as the server
// ordered them. Bodies stay in -o json; the table answers "what happened to each mail".
func TenantReplies(w io.Writer, list *client.TenantReplyList) {
	if len(list.Items) == 0 {
		_, _ = fmt.Fprintln(w, "(no answered mails)")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "RECEIVED\tFROM\tSUBJECT\tOUTCOME\tSCORE\tRUN")
	for _, it := range list.Items {
		received, from := it.CreatedAt, "-"
		if it.Trigger != nil {
			received = orDash(it.Trigger.Date, it.CreatedAt)
			from = orDash(it.Trigger.FromAddress, "-")
		}
		score := "-"
		if it.Feedback != nil && it.Feedback.Score != nil {
			score = fmt.Sprint(*it.Feedback.Score)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", received, from, clipStr(orDash(it.Subject, "-"), 40), replyOutcome(it), score, clipID(it.RunID, 8))
	}
	_ = tw.Flush()
	if list.NextCursor != "" {
		_, _ = fmt.Fprintf(w, "more: --cursor %s\n", list.NextCursor)
	}
}

// replyOutcome mirrors the server's decomposed send state without inferring "unsent" from a missing latch.
func replyOutcome(it client.TenantReply) string {
	out := it.DraftStatus
	// AutoSent first: our own synced send also fills Sent (edited unknown), and it is not a human send.
	switch {
	case it.AutoSent:
		out = "auto-sent"
	case it.Sent != nil && it.Sent.Edited == nil:
		out = "sent"
	case it.Sent != nil && *it.Sent.Edited:
		out = "sent (edited)"
	case it.Sent != nil:
		out = "sent as proposed"
	}
	if it.Shadow {
		out += " [shadow]"
	}
	if it.BodiesScrubbed {
		out += " [scrubbed]"
	}
	return out
}

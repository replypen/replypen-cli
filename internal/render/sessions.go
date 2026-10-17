package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/digest"
)

// sessionLabelChars bounds the label column: a conversation list is for recognising a conversation, not
// for reading it — the transcript is one `rp run thread <session_id>` away.
const sessionLabelChars = 60

// Sessions renders the conversation table of `rp run sessions`. One row per chat conversation, newest
// first, ending in the session id so the next drill command can be copy-pasted.
func Sessions(w io.Writer, resp *client.SessionsResponse) {
	if len(resp.Sessions) == 0 {
		_, _ = fmt.Fprintln(w, "No conversations.")
		return
	}
	// The TIER column appears only once the server ships it, so older payloads keep their exact layout.
	withTier := false
	for _, s := range resp.Sessions {
		withTier = withTier || s.Tier != ""
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	tierHead := ""
	if withTier {
		tierHead = "TIER\t"
	}
	_, _ = fmt.Fprintln(tw, "CREATED\tTENANT\tTURNS\t"+tierHead+"FEEDBACK\tCONVERSATION\tSESSION")
	for _, s := range resp.Sessions {
		tier := ""
		if withTier {
			tier = digest.TierLabel(s.Tier, s.TierSource) + "\t"
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%d\t%s%s\t%s\t%s\n",
			s.CreatedAt, dash(s.TenantSlug), s.Turns, tier, feedbackLabel(s.Feedback),
			sessionLabel(s), s.SessionID)
	}

	_ = tw.Flush()
	if resp.NextBefore != "" {
		_, _ = fmt.Fprintf(w, "\nMore: rp run sessions --before %s\n", resp.NextBefore)
	}
}

// sessionLabel prefers the conversation's own title and falls back to its opening question, so an
// untitled conversation is still recognisable.
func sessionLabel(s client.SessionSummary) string {
	label := strings.TrimSpace(s.Title)
	if label == "" {
		label = strings.TrimSpace(s.FirstQuestion)
	}
	if label == "" {
		return "-"
	}
	label = strings.Join(strings.Fields(label), " ")
	if r := []rune(label); len(r) > sessionLabelChars {
		label = strings.TrimRight(string(r[:sessionLabelChars]), " ") + "…"
	}
	return label
}

// feedbackLabel compresses the rating rollup into one cell: the score (a range when the turns
// disagree), a 💬 when someone also wrote why, "-" when nobody rated the conversation at all.
func feedbackLabel(f *client.SessionFeedback) string {
	if f == nil || f.Count == 0 {
		return "-"
	}
	score := fmt.Sprintf("%d★", f.MaxScore)
	if f.MinScore > 0 && f.MinScore != f.MaxScore {
		score = fmt.Sprintf("%d–%d★", f.MinScore, f.MaxScore)
	}
	if f.MaxScore == 0 {
		score = "rated"
	}
	if f.HasComment {
		score += "💬"
	}
	return score
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/digest"
)

// TokenList renders `rp project token ls`: the caller has already filtered and ordered items
// (digest.SelectTokens). Columns tolerate a server that predates last-used tracking (empty cells, LAST
// USED "never"). wide adds SCOPE and USER AGENT, which are otherwise only in -o json.
func TokenList(w io.Writer, items []client.Item, now time.Time, wide bool) {
	if len(items) == 0 {
		_, _ = fmt.Fprintln(w, "(none)")
		return
	}
	header := []string{"ID", "LABEL", "SUBJECT", "TENANT", "STATUS", "CREATED", "LAST USED", "LAST IP", "VIA"}
	if wide {
		header = append(header, "SCOPE", "USER AGENT")
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, it := range items {
		s := func(key string, max int) string { return orDash(truncate(digest.TokenString(it, key), max), "-") }
		row := []string{
			s("id", 40), s("label", 24), s("subject", 32), s("tenant", 20), s("status", 12),
			tokenCreated(it), tokenLastUsed(it, now), s("last_used_ip", 39), s("last_used_route", 32),
		}
		if wide {
			row = append(row, s("scope", 60), s("last_used_user_agent", 48))
		}
		_, _ = fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
}

// TokenFleetList renders `rp project token ls --all`: machine credentials across every project, with
// owner and expiry. project "" is an all-projects credential. The user agent is clipped unless wide;
// wide also adds the grant ID (revoke stays per project: rp project token revoke <id> --project <p>).
func TokenFleetList(w io.Writer, items []client.Item, now time.Time, wide bool) {
	if len(items) == 0 {
		_, _ = fmt.Fprintln(w, "(none)")
		return
	}
	header := []string{"PROJECT", "TENANT", "LABEL", "OWNER", "SCOPE", "STATUS", "CREATED", "EXPIRES", "LAST USED", "VIA", "AGENT", "LAST IP"}
	agentMax := 24
	if wide {
		header = append(header, "ID")
		agentMax = 80
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, it := range items {
		s := func(key string, max int) string { return orDash(truncate(digest.TokenString(it, key), max), "-") }
		project := orDash(truncate(digest.TokenString(it, "project"), 24), "(all projects)")
		row := []string{
			project, s("tenant", 20), s("label", 24), s("owner", 32), s("scope", 24), s("status", 12),
			tokenCreated(it), tokenDate(it, "expires_at"), tokenLastUsed(it, now), s("last_used_route", 32),
			s("last_used_user_agent", agentMax), s("last_used_ip", 39),
		}
		if wide {
			row = append(row, s("id", 40))
		}
		_, _ = fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
}

func tokenDate(it client.Item, key string) string {
	if t, ok := digest.TokenTime(it, key); ok {
		return t.UTC().Format("2006-01-02")
	}
	return "-"
}

func tokenCreated(it client.Item) string { return tokenDate(it, "created_at") }

func tokenLastUsed(it client.Item, now time.Time) string {
	t, ok := digest.TokenTime(it, "last_used_at")
	if !ok {
		return "never"
	}
	return relativeAgo(now.Sub(t))
}

// relativeAgo spells an elapsed duration as the coarsest whole unit ("just now", "5m ago", "3h ago",
// "12d ago"). A negative duration (clock skew) reads as "just now".
func relativeAgo(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	}
}

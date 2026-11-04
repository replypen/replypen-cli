// This file renders `rp project mailbox thread`: one conversation re-read live from the mailbox, as a
// Markdown document (the file the command writes) plus a body-free index for stdout.
package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/replypen/replypen-cli/internal/client"
)

// MailThreadMarkdown renders every message in order: direction, date, from/to/cc, subject, attachment
// names and the cleaned body.
func MailThreadMarkdown(t *client.MailThread) []byte {
	var b strings.Builder
	subject := ""
	if len(t.Messages) > 0 {
		subject = t.Messages[0].Subject
	}
	fmt.Fprintf(&b, "# %s\n\n", orDash(subject, "(no subject)"))
	fmt.Fprintf(&b, "- mailbox: %s (%s, %s)\n", t.MailboxAddress, t.Provider, t.MailboxID)
	fmt.Fprintf(&b, "- thread: %s\n", t.ExternalThreadID)
	if t.RunID != "" {
		fmt.Fprintf(&b, "- run: %s\n", t.RunID)
	}
	fmt.Fprintf(&b, "- fetched live: %s · %d messages\n", t.FetchedAt, len(t.Messages))
	for i, m := range t.Messages {
		fmt.Fprintf(&b, "\n## %d · %s · %s\n\n", i+1, m.Direction, m.Date)
		fmt.Fprintf(&b, "- from: %s\n", m.From)
		if len(m.To) > 0 {
			fmt.Fprintf(&b, "- to: %s\n", strings.Join(m.To, ", "))
		}
		if len(m.Cc) > 0 {
			fmt.Fprintf(&b, "- cc: %s\n", strings.Join(m.Cc, ", "))
		}
		if m.AuthorKind != "" {
			fmt.Fprintf(&b, "- author: %s\n", m.AuthorKind)
		}
		fmt.Fprintf(&b, "- subject: %s\n", m.Subject)
		if names := attachmentNames(m.Attachments); names != "" {
			fmt.Fprintf(&b, "- attachments: %s\n", names)
		}
		fmt.Fprintf(&b, "\n%s\n", strings.TrimSpace(m.Body))
	}
	return []byte(b.String())
}

// MailThreadIndex prints where the Markdown landed and one body-free line per message, so stdout (often
// an agent transcript) never carries the mail text itself.
func MailThreadIndex(w io.Writer, t *client.MailThread, path string) {
	_, _ = fmt.Fprintf(w, "%d messages · thread %s · %s\n", len(t.Messages), t.ExternalThreadID, t.MailboxAddress)
	for i, m := range t.Messages {
		line := fmt.Sprintf("%2d  %-8s  %s", i+1, m.Direction, m.Date)
		if n := len(m.Attachments); n > 0 {
			line += fmt.Sprintf("  📎%d", n)
		}
		_, _ = fmt.Fprintln(w, line)
	}
	_, _ = fmt.Fprintf(w, "full thread (chmod 600): %s\n", path)
}

func attachmentNames(as []client.MailThreadAttachment) string {
	names := make([]string, 0, len(as))
	for _, a := range as {
		name := orDash(a.Filename, "(unnamed)")
		if a.Inline {
			name += " (inline)"
		}
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

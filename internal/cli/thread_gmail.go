package cli

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/idutil"
	"github.com/replypen/replypen-cli/internal/render"
)

// isGmailURL reports a pasted Gmail web/print link (any /u/N/ account), as opposed to a run link.
func isGmailURL(arg string) bool {
	u, err := url.Parse(strings.TrimSpace(arg))
	return err == nil && looksLikeURL(strings.TrimSpace(arg)) && u.Host == "mail.google.com"
}

// gmailThreadHex turns any Gmail link shape into the hex thread id ReplyPen stores as external_thread_id.
func gmailThreadHex(arg string) (string, error) {
	res := idutil.ClassifyGmail(arg)
	if res.Hex == "" {
		return "", fmt.Errorf("no Gmail thread id in this link (%s). Open the mail, choose ⋮ → Print all, "+
			"and paste that print URL (it carries permthid=thread-f:…)", orNote(res))
	}
	return res.Hex, nil
}

func orNote(r idutil.GmailResult) string {
	if r.Note != "" {
		return r.Kind + ": " + r.Note
	}
	return r.Kind
}

// gmailMailboxContext adds what a Gmail link alone cannot say: the scoped project's Google mailboxes and,
// without a visible record, the likely ingestion gap.
func gmailMailboxContext(e *env, tr *client.ThreadTrace) {
	project := e.scopeProject()
	if project == "" {
		_, _ = fmt.Fprintln(e.out, "\nPass --project to check which Google mailbox should have ingested this thread.")
		return
	}
	c, err := e.newClient()
	if err != nil {
		return
	}
	list, _, err := c.WatchedMailboxes(e.ctx(), project, e.scopeTenant())
	if err != nil {
		_, _ = fmt.Fprintf(e.err, "warning: mailbox lookup failed: %v\n", err)
		return
	}
	render.GmailMailboxContext(e.out, project, tr, list.Mailboxes)
}

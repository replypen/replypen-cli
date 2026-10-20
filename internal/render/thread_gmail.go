package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/replypen/replypen-cli/internal/client"
)

// GmailMailboxContext follows a Gmail-link thread trace: the project's Google mailboxes (provider-filtered,
// so an Intercom connection on the same address never reads as a match) and, when the trace is empty, the
// likely ingestion gap. Empty is also what a login without run visibility gets, hence "visible".
func GmailMailboxContext(w io.Writer, project string, tr *client.ThreadTrace, mailboxes []client.WatchedMailbox) {
	var google []client.WatchedMailbox
	for _, m := range mailboxes {
		if m.Provider == "google" {
			google = append(google, m)
		}
	}
	_, _ = fmt.Fprintf(w, "\nGoogle mailbox(es) in %s:\n", project)
	for _, m := range google {
		lastSync := m.LastSuccessfulSyncAt
		if lastSync == "" {
			lastSync = "never"
		}
		_, _ = fmt.Fprintf(w, "  %s  status=%s mode=%s processing=%t last_sync=%s\n", m.EmailAddress,
			m.Status, m.Mode, m.ProcessingEnabled, lastSync)
	}
	if len(tr.Threads) > 0 || len(tr.Runs) > 0 {
		return
	}
	_, _ = fmt.Fprintf(w, "\nNo visible record of this Gmail thread in %s: never processed, or your login cannot see "+
		"this mailbox's runs (ask a project admin, or rerun with a run link). If never processed, likely:\n", project)
	if len(google) == 0 {
		_, _ = fmt.Fprintln(w, "  - no Google mailbox is connected to this project (wrong --project, or the account is not connected)")
		return
	}
	specific := false
	for _, m := range google {
		var why []string
		if m.Status != "active" {
			why = append(why, "status "+m.Status)
		}
		if m.Mode == "off" {
			why = append(why, "mode off")
		}
		if !m.ProcessingEnabled {
			why = append(why, "processing disabled (silent onboarding)")
		}
		if m.ConsecutiveSyncFailures > 0 {
			why = append(why, fmt.Sprintf("%d consecutive sync failures", m.ConsecutiveSyncFailures))
		}
		if len(why) > 0 {
			specific = true
			_, _ = fmt.Fprintf(w, "  - %s: %s\n", m.EmailAddress, strings.Join(why, ", "))
		}
	}
	if !specific {
		_, _ = fmt.Fprintln(w, "  - the link's Gmail account (/u/N/) is not one of the mailboxes above\n"+
			"  - the mail arrived before the mailbox was connected, or outside what it watches (archived, filtered, category)\n"+
			"  - ingestion dropped it before triage (own/suppressed sender, automated bulk mail); not visible via the public API")
	}
}

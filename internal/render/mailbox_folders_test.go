package render

import (
	"bytes"
	"strings"
	"testing"

	"github.com/replypen/replypen-cli/internal/client"
)

func TestIMAPFoldersMarksSentAndIncomplete(t *testing.T) {
	var out bytes.Buffer
	IMAPFolders(&out, &client.IMAPFoldersResponse{Incomplete: true, Folders: []client.IMAPFolder{
		{Name: "INBOX.Sent", SentLike: true, Messages: 100, Recent: 16, FromMailbox: 98, RecentFromMailbox: 16, OldestFromMailbox: "2020-11-23", OldestDate: "2020-11-23", NewestDate: "2026-11-20"},
		{Name: "Klanten", Error: "not selectable"},
	}})
	got := out.String()
	for _, want := range []string{"INBOX.Sent", "yes", "98 / 16 since 2020-11-23", "2020-11-23 .. 2026-11-20", "not selectable", "incomplete:"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output lacks %q:\n%s", want, got)
		}
	}
}

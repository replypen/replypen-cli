package render

import (
	"testing"

	"github.com/replypen/replypen-cli/internal/client"
)

func TestReplyOutcome(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name string
		it   client.TenantReply
		want string
	}{
		// Our own synced send fills Sent with edited unknown; it must still read as auto-sent.
		{"auto-sent with synced copy", client.TenantReply{DraftStatus: "sent", AutoSent: true, Sent: &client.TenantReplySent{}}, "auto-sent"},
		{"edit unknown", client.TenantReply{DraftStatus: "placed", Sent: &client.TenantReplySent{}}, "sent"},
		{"edited", client.TenantReply{DraftStatus: "placed", Sent: &client.TenantReplySent{Edited: &yes}}, "sent (edited)"},
		{"as proposed", client.TenantReply{DraftStatus: "placed", Sent: &client.TenantReplySent{Edited: &no}}, "sent as proposed"},
		{"not sent", client.TenantReply{DraftStatus: "placed"}, "placed"},
		{"shadow", client.TenantReply{DraftStatus: "suppressed", Shadow: true}, "suppressed [shadow]"},
	} {
		if got := replyOutcome(tc.it); got != tc.want {
			t.Errorf("%s: replyOutcome = %q, want %q", tc.name, got, tc.want)
		}
	}
}

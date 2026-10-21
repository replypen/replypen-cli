package digest

import (
	"strings"
	"testing"

	"github.com/replypen/replypen-cli/internal/client"
)

func TestPhoneLabSignals(t *testing.T) {
	ms := int64(2001)
	cases := []struct {
		name   string
		events []client.PhoneLabEvent
		want   string
		absent string
	}{
		{"missed lookup", []client.PhoneLabEvent{{T: 84000, Kind: "transcript", Who: "agent", Text: "Un instant, je regarde les créneaux."}, {T: 122000, Kind: "transcript", Who: "caller", Text: "Est-ce que vous êtes toujours là ?"}}, "lookup filler without delegation", ""},
		{"timely delegation unsorted", []client.PhoneLabEvent{{T: 4000, Kind: "tool", ExecMS: &ms}, {T: 1000, Kind: "transcript", Who: "agent", Text: "Un instant"}, {T: 3000, Kind: "delegation", Status: "created", Delegation: "d"}}, "slow tool", "lookup filler"},
		{"already busy", []client.PhoneLabEvent{{T: 0, Kind: "delegation", Status: "created", Delegation: "d"}, {T: 5000, Kind: "transcript", Who: "agent", Text: "Un instant"}, {T: 10000, Kind: "commentary", Status: "sent", Delegation: "d"}}, "", "lookup filler"},
		{"finished delegation", []client.PhoneLabEvent{{T: 0, Kind: "delegation", Status: "created", Delegation: "d"}, {T: 1000, Kind: "commentary", Status: "sent", Delegation: "d"}, {T: 5000, Kind: "transcript", Who: "agent", Text: "Un instant"}, {T: 10000, Kind: "note"}}, "lookup filler", ""},
		{"not yet overdue", []client.PhoneLabEvent{{T: 1000, Kind: "transcript", Who: "agent", Text: "Un instant"}, {T: 3999, Kind: "note"}}, "", "lookup filler"},
		{"capital accented concern", []client.PhoneLabEvent{{Kind: "transcript", Who: "caller", Text: "Êtes-vous là ?"}}, "caller checks", ""},
		{"capital accented still there", []client.PhoneLabEvent{{Kind: "transcript", Who: "caller", Text: "Êtes-vous toujours là ?"}}, "caller checks", ""},
		{"lowercase still there", []client.PhoneLabEvent{{Kind: "transcript", Who: "caller", Text: "êtes-vous toujours là ?"}}, "caller checks", ""},
		{"caller concern", []client.PhoneLabEvent{{Kind: "transcript", Who: "caller", Text: "Vous êtes toujours là ?"}}, "caller checks", ""},
		{"no audio", []client.PhoneLabEvent{{Kind: "voicemail", Status: "done", Outcome: "no_audio"}}, "voicemail done no_audio", ""},
		{"failed voicemail post", []client.PhoneLabEvent{{Kind: "voicemail", Status: "done", Outcome: "http_500"}}, "voicemail done http_500", ""},
		{"saved voicemail", []client.PhoneLabEvent{{Kind: "voicemail", Status: "done", Outcome: "saved"}}, "", "voicemail"},
		{"successful voicemail", []client.PhoneLabEvent{{Kind: "voicemail", Status: "done", Outcome: "created"}}, "", "voicemail"},
		{"audio being transcribed", []client.PhoneLabEvent{{Kind: "voicemail", Status: "done", Outcome: "transcribing"}}, "", "voicemail"},
		{"empty voicemail", []client.PhoneLabEvent{{Kind: "voicemail", Status: "done", Outcome: "empty"}}, "voicemail done empty", ""},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			signals := PhoneLabSignals(&client.PhoneLabRun{Events: tt.events})
			var all strings.Builder
			for _, s := range signals {
				if s.Index < 0 || s.Index >= len(tt.events) {
					t.Fatal("invalid original event index")
				}
				all.WriteString(s.Text)
			}
			if tt.want != "" && !strings.Contains(all.String(), tt.want) {
				t.Fatalf("missing %q in %s", tt.want, all.String())
			}
			if tt.absent != "" && strings.Contains(all.String(), tt.absent) {
				t.Fatalf("unexpected %q in %s", tt.absent, all.String())
			}
		})
	}
}

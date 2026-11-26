package digest

import (
	"reflect"
	"testing"
	"time"

	"github.com/replypen/replypen-cli/internal/client"
)

func phoneFixture() *client.PhoneLabRun {
	start := time.Date(2026, 11, 26, 12, 56, 43, 0, time.UTC)
	end := start.Add(131 * time.Second)
	one := 1
	r := &client.PhoneLabRun{RunID: "fd93d291-db37-5c89-a669-c809a31b455e", CreatedAt: start, EndedAt: &end, Events: []client.PhoneLabEvent{
		{T: 18000, Kind: "transcript", Who: "caller", Text: "Ik wil mijn afspraak verplaatsen."},
		{T: 5000, Kind: "transcript", Who: "agent", Text: "Waarmee kan ik u helpen?"}, // out of order: sorted by t
		{T: 400, Kind: "tool", Status: "done", Outcome: "ok", Cmd: "python /brain/skills/phone/scripts/preload.py", Stdout: `{"status": "ok"}`},
		{T: 60000, Kind: "tool", Status: "done", Outcome: "ok", Cmd: `python /brain/skills/phone/scripts/move.py --id "1" --start "x"`, Stdout: `{"status": "confirm", "label": "dinsdag"}`},
		{T: 88000, Kind: "tool", Status: "done", Outcome: "ok", Cmd: `python /brain/skills/phone/scripts/move.py --id "1" --start "x" --yes`, Stdout: "log line\n" + `{"status": "moved", "label": "maandag 30 november om 14 uur"}`},
		{T: 89000, Kind: "tool", Status: "done", Outcome: "ok", Cmd: `python /brain/skills/phone/scripts/move.py --id "1" --start "x" --yes`, Stdout: `{"status": "moved", "label": "maandag 30 november om 14 uur"}`},
		{T: 95000, Kind: "tool", Status: "done", Outcome: "ok", ExitCode: &one, Cmd: `python /brain/skills/phone/scripts/cancel.py --id "1" --yes`, Stdout: `{"status": "cancelled"}`},
		{T: 96000, Kind: "tool", Status: "started", Cmd: `python /brain/skills/phone/scripts/book.py`},
		{T: 97000, Kind: "tool", Status: "done", Outcome: "ok", Cmd: "cat /brain/AGENTS.md"},
		{T: 118000, Kind: "tool", Status: "done", Outcome: "ok", Cmd: `python /brain/skills/phone/scripts/message.py --question "pijn"`, Stdout: `{"status": "noted"}`},
		{T: 120000, Kind: "voicemail", Status: "done", Outcome: "saved"},
		{T: 121000, Kind: "transcript", Who: "caller", Text: "  "},
		{T: 122000, Kind: "commentary", Text: "not spoken"},
	}}
	r.Caller.Phone = "+32485683991"
	return r
}

func TestDigestPhoneCallOutcomes(t *testing.T) {
	c := DigestPhoneCall(phoneFixture(), false)
	want := []PhoneOutcome{{Kind: "moved", Label: "maandag 30 november om 14 uur"}, {Kind: "message"}}
	if !reflect.DeepEqual(c.Outcomes, want) {
		t.Fatalf("outcomes = %+v, want %+v (a preview, a failed cancel and a retry are no outcome)", c.Outcomes, want)
	}
	if c.Outcome() != "moved+message" || c.Turns != 1 || c.Caller != "***991" || c.Duration != 131*time.Second {
		t.Fatalf("got outcome=%q turns=%d caller=%q duration=%s", c.Outcome(), c.Turns, c.Caller, c.Duration)
	}
	if len(c.Lines) != 2 || c.Lines[0].Who != "agent" {
		t.Fatalf("without steps only spoken lines, in time order: %+v", c.Lines)
	}
}

func TestDigestPhoneCallSteps(t *testing.T) {
	c := DigestPhoneCall(phoneFixture(), true)
	var steps []PhoneLine
	for _, l := range c.Lines {
		if l.Who == "step" {
			steps = append(steps, l)
		}
	}
	got := make([][3]any, 0, len(steps))
	for _, s := range steps {
		got = append(got, [3]any{s.Text, s.Status, s.Failed})
	}
	want := [][3]any{
		{"preload", "ok", false},
		{`move --id "1" --start "x"`, "confirm", false},
		{`move --id "1" --start "x" --yes`, "moved", false},
		{`move --id "1" --start "x" --yes`, "moved", false},
		{`cancel --id "1" --yes`, "exit 1", true},
		{`message --question "pijn"`, "noted", false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("steps = %v\nwant %v", got, want)
	}
}

func TestDigestPhoneCallSilent(t *testing.T) {
	r := &client.PhoneLabRun{RunID: "x", Events: []client.PhoneLabEvent{{Kind: "transcript", Who: "agent", Text: "Hallo?"}}}
	if c := DigestPhoneCall(r, false); c.Outcome() != "hung up" || c.Duration != 0 || c.Caller != "—" {
		t.Fatalf("silent caller: %+v outcome %q", c, c.Outcome())
	}
	r.Events = append(r.Events, client.PhoneLabEvent{Kind: "transcript", Who: "caller", Text: "Hoeveel kost een controle?"})
	if c := DigestPhoneCall(r, false); c.Outcome() != "no action" {
		t.Fatalf("talk only: %q", c.Outcome())
	}
}

func TestMaskPhone(t *testing.T) {
	for in, want := range map[string]string{"+32485683991": "***991", "": "—", "anonymous": "—", "12": "***", "+32 485 68 39 91": "***991"} {
		if got := MaskPhone(in); got != want {
			t.Errorf("MaskPhone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDigestPhoneCallStatusEdges(t *testing.T) {
	tool := func(script, status string) client.PhoneLabEvent {
		return client.PhoneLabEvent{Kind: "tool", Status: "done", Outcome: "ok", Cmd: "python /brain/skills/phone/scripts/" + script + ".py --yes", Stdout: `{"status": "` + status + `"}`}
	}
	for _, tc := range []struct {
		name string
		ev   client.PhoneLabEvent
		want string
	}{
		{"move refused: appointment changed", tool("move", "changed"), "no action"},
		{"cancel refused: appointment changed", tool("cancel", "changed"), "no action"},
		{"reservation booked after the call", tool("book", "pending"), "booked"},
		{"retry of that reservation", tool("book", "already_reserved"), "booked"},
		{"waitlist sms", tool("waitlist", "sent"), "waitlist"},
		{"fr move done", tool("move", "done"), "moved"},
		{"voicemail saved", client.PhoneLabEvent{Kind: "voicemail", Status: "done", Outcome: "saved"}, "message"},
		{"voicemail without audio", client.PhoneLabEvent{Kind: "voicemail", Status: "done", Outcome: "no_audio"}, "no action"},
		{"voicemail withdrawn", client.PhoneLabEvent{Kind: "voicemail", Status: "done", Outcome: "handed_back"}, "no action"},
		{"voicemail nothing reached", client.PhoneLabEvent{Kind: "voicemail", Status: "done"}, "no action"},
	} {
		r := &client.PhoneLabRun{Events: []client.PhoneLabEvent{{Kind: "transcript", Who: "caller", Text: "ja"}, tc.ev}}
		if got := DigestPhoneCall(r, false).Outcome(); got != tc.want {
			t.Errorf("%s: outcome %q, want %q", tc.name, got, tc.want)
		}
	}
}

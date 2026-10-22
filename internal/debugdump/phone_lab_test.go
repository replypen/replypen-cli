package debugdump

import (
	"strings"
	"testing"

	"github.com/replypen/replypen-cli/internal/client"
)

func TestPhoneLabLatencyUsesLastCallerDelta(t *testing.T) {
	latency := int64(1000)
	r := &client.PhoneLabRun{Events: []client.PhoneLabEvent{
		{T: 0, Kind: "transcript", Who: "caller", Text: "A long question"},
		{T: 8000, Kind: "transcript", Who: "agent", Text: "Answer", LatencyMS: &latency},
	}}
	index := PhoneLabIndex(r)
	if strings.Contains(index, "transcript latency 8000") || !strings.Contains(index, "No automatic signals") {
		t.Fatalf("caller duration flagged as waiting time: %s", index)
	}
}

func TestPhoneLabSuccessfulActionHasNoFailureSignal(t *testing.T) {
	for _, outcome := range []string{"ok", "preflight_ok", "would_execute"} {
		r := &client.PhoneLabRun{Events: []client.PhoneLabEvent{{Kind: "tool", Status: "done", Outcome: outcome}}}
		if index := PhoneLabIndex(r); !strings.Contains(index, "No automatic signals") {
			t.Fatalf("successful %s flagged: %s", outcome, index)
		}
	}
}

func TestPhoneLabToolCommandKeepsItsFlags(t *testing.T) {
	cmd := `python /brain/skills/phone/scripts/book.py --start "2026-11-05T09:00:00+01:00" --first "Marc" --last "Dubois" --dob "1979-03-03" --mobile "0475123456" --email "marc.dubois@gmail.com"`
	r := &client.PhoneLabRun{Events: []client.PhoneLabEvent{{Kind: "tool", Status: "done", Name: "bash", Cmd: cmd}}}
	if index := PhoneLabIndex(r); !strings.Contains(index, `--email "marc.dubois@gmail.com"`) {
		t.Fatalf("tool command clipped: %s", index)
	}
}

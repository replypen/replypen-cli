package render

import (
	"bytes"
	"testing"
	"time"

	"github.com/replypen/replypen-cli/internal/digest"
)

func phoneCallFixture() digest.PhoneCall {
	return digest.PhoneCall{
		RunID: "fd93d291-db37-5c89-a669-c809a31b455e", StartedAt: time.Date(2026, 11, 26, 12, 56, 43, 0, time.UTC),
		Duration: 131 * time.Second, Caller: "***991", Turns: 2,
		Outcomes: []digest.PhoneOutcome{{Kind: "moved", Label: "maandag 30 november om 14 uur"}, {Kind: "message"}},
		Lines: []digest.PhoneLine{
			{T: 5 * time.Second, Who: "agent", Text: "U spreekt met de digitale assistent. Waarmee kan ik u helpen? Ik kan een afspraak maken, verplaatsen of annuleren."},
			{T: 18 * time.Second, Who: "caller", Text: "Ik wil mijn afspraak verplaatsen."},
			{T: 88 * time.Second, Who: "step", Text: `move --id "1" --yes`, Status: "moved"},
			{T: 95 * time.Second, Who: "step", Text: `cancel --id "1"`, Status: "exit 1", Failed: true},
			{T: 102 * time.Second, Who: "caller", Text: "Dank u."},
		},
	}
}

func TestPhoneCallGolden(t *testing.T) {
	var b bytes.Buffer
	PhoneCall(&b, phoneCallFixture(), "dentai-phone-nl", "demo", true, time.UTC)
	assertGolden(t, "phone_call.golden", b.String())
}

func TestPhoneCallsGolden(t *testing.T) {
	silent := digest.PhoneCall{RunID: "17dddd4d-aced-51d1-bf7b-36e5fc1fbcfc", StartedAt: time.Date(2026, 11, 24, 11, 41, 34, 0, time.UTC), Caller: "***429"}
	var b bytes.Buffer
	PhoneCalls(&b, []digest.PhoneCall{phoneCallFixture(), silent}, "dentai-phone-nl", "demo", time.UTC)
	assertGolden(t, "phone_calls.golden", b.String())
}

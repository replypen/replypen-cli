package digest

import (
	"testing"

	"github.com/replypen/replypen-cli/internal/client"
)

func TestScoreTriageEvalClasses(t *testing.T) {
	yes, no := true, false
	att := func(ps ...bool) []client.TriageEvalAttempt {
		out := make([]client.TriageEvalAttempt, len(ps))
		for i, p := range ps {
			out[i] = client.TriageEvalAttempt{ShouldProcess: p}
		}
		return out
	}
	failOpen := []client.TriageEvalAttempt{{ShouldProcess: true, FailedOpen: true}}
	rep := client.TriageEvalReport{Results: []client.TriageEvalCaseResult{
		{ID: "pass", Status: "approved", Expect: client.TriageEvalExpect{Process: &yes}, Attempts: att(true, true)},
		{ID: "extra", Expect: client.TriageEvalExpect{Process: &no}, Attempts: att(true)},
		{ID: "flaky", Expect: client.TriageEvalExpect{Process: &yes}, Attempts: att(true, false)},
		{ID: "skip", Expect: client.TriageEvalExpect{Process: &yes}, Attempts: att(false)},
		{ID: "error", Expect: client.TriageEvalExpect{Process: &yes}, Attempts: failOpen},
	}}
	s := ScoreTriageEval(rep)
	want := []string{"skip:false_skip", "flaky:flaky", "error:failed_open", "extra:extra_process", "pass:pass"}
	for i, c := range s.Cases {
		if got := c.Case.ID + ":" + c.Class; got != want[i] {
			t.Fatalf("case %d = %s, want %s (worst first)", i, got, want[i])
		}
	}
	if s.Pass != 1 || s.FalseSkip != 1 || s.Flaky != 1 || s.FailOpen != 1 || s.ExtraProcess != 1 || s.Proposed != 4 {
		t.Fatalf("counts = %+v", s)
	}
	if ch := CompareTriageEval(s, s); len(ch) != 0 {
		t.Fatalf("self-compare changes = %v", ch)
	}
}

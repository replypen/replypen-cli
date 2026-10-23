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
		// One errored attempt must not hide the false skip its sibling decided.
		{ID: "errskip", Expect: client.TriageEvalExpect{Process: &yes}, Attempts: append(failOpen, client.TriageEvalAttempt{Reason: "newsletter"})},
		// No attempt is no judgment: never counted as a pass.
		{ID: "none", Expect: client.TriageEvalExpect{Process: &yes}},
	}}
	s := ScoreTriageEval(rep)
	want := []string{"errskip:false_skip", "skip:false_skip", "flaky:flaky", "error:failed_open", "none:failed_open", "extra:extra_process", "pass:pass"}
	for i, c := range s.Cases {
		if got := c.Case.ID + ":" + c.Class; got != want[i] {
			t.Fatalf("case %d = %s, want %s (worst first)", i, got, want[i])
		}
	}
	if s.Pass != 1 || s.FalseSkip != 2 || s.Flaky != 1 || s.FailOpen != 2 || s.ExtraProcess != 1 || s.Proposed != 6 {
		t.Fatalf("counts = %+v", s)
	}
	if c := s.Cases[0]; c.Case.Attempts[c.Evidence].Reason != "newsletter" {
		t.Fatalf("errskip evidence = attempt %d, want the judged skip", c.Evidence)
	}
	if ch := CompareTriageEval(s, s); len(ch) != 0 {
		t.Fatalf("self-compare changes = %v", ch)
	}
}

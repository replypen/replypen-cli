package digest

import (
	"sort"

	"github.com/replypen/replypen-cli/internal/client"
)

// Triage eval verdict classes, worst first: a false skip loses patient work, an extra process only
// costs a draft a human discards.
const (
	TriageFalseSkip    = "false_skip"    // expected process, decided skip
	TriageExtraProcess = "extra_process" // expected skip, decided process
	TriageFlaky        = "flaky"         // repeated attempts disagree
	TriageFailedOpen   = "failed_open"   // classifier errored; the "process" is not a judgment
	TriagePass         = "pass"
)

var triageClassRank = map[string]int{TriageFalseSkip: 0, TriageFlaky: 1, TriageFailedOpen: 2, TriageExtraProcess: 3, TriagePass: 4}

// TriageCaseScore is one case's verdict over its attempts.
type TriageCaseScore struct {
	Case      client.TriageEvalCaseResult
	Class     string
	Processed int // attempts that decided process (fail-open ones included)
	Evidence  int // index of the attempt that explains Class: the first that disagrees with expect or failed open
}

// TriageEvalScore is the whole report's scorecard.
type TriageEvalScore struct {
	Cases                                          []TriageCaseScore // worst first, then by id
	Pass, FalseSkip, ExtraProcess, Flaky, FailOpen int
	Proposed                                       int // cases whose labels the owner has not approved yet
}

// ScoreTriageEval classifies every case: all attempts must agree with expect.process to pass. A
// fail-open attempt is no judgment, so the class comes from the judged attempts; failed_open only when
// those agree (or none exist) — one errored attempt must not hide a false skip in its sibling.
func ScoreTriageEval(rep client.TriageEvalReport) TriageEvalScore {
	var s TriageEvalScore
	for _, r := range rep.Results {
		cs := TriageCaseScore{Case: r, Class: TriagePass, Evidence: -1}
		want := r.Expect.Process != nil && *r.Expect.Process
		judged, judgedProcess, firstFailed := 0, 0, -1
		for i, a := range r.Attempts {
			if a.ShouldProcess {
				cs.Processed++
			}
			if a.FailedOpen {
				if firstFailed < 0 {
					firstFailed = i
				}
				continue
			}
			judged++
			if a.ShouldProcess {
				judgedProcess++
			}
			if a.ShouldProcess != want && cs.Evidence < 0 {
				cs.Evidence = i
			}
		}
		switch {
		case judgedProcess > 0 && judgedProcess < judged:
			cs.Class = TriageFlaky
		case judged > 0 && want && judgedProcess == 0:
			cs.Class = TriageFalseSkip
		case judged > 0 && !want && judgedProcess > 0:
			cs.Class = TriageExtraProcess
		case firstFailed >= 0:
			cs.Class, cs.Evidence = TriageFailedOpen, firstFailed
		}
		if cs.Evidence < 0 {
			cs.Evidence = len(r.Attempts) - 1
		}
		switch cs.Class {
		case TriagePass:
			s.Pass++
		case TriageFalseSkip:
			s.FalseSkip++
		case TriageExtraProcess:
			s.ExtraProcess++
		case TriageFlaky:
			s.Flaky++
		case TriageFailedOpen:
			s.FailOpen++
		}
		if r.Status != "approved" {
			s.Proposed++
		}
		s.Cases = append(s.Cases, cs)
	}
	sort.SliceStable(s.Cases, func(i, j int) bool {
		a, b := s.Cases[i], s.Cases[j]
		if triageClassRank[a.Class] != triageClassRank[b.Class] {
			return triageClassRank[a.Class] < triageClassRank[b.Class]
		}
		return a.Case.ID < b.Case.ID
	})
	return s
}

// TriageEvalChange is one case whose verdict class moved between a baseline and a candidate.
type TriageEvalChange struct {
	ID, Before, After string
}

// CompareTriageEval lists the cases whose class differs, worst candidate outcome first.
func CompareTriageEval(base, cand TriageEvalScore) []TriageEvalChange {
	before := map[string]string{}
	for _, c := range base.Cases {
		before[c.Case.ID] = c.Class
	}
	var out []TriageEvalChange
	for _, c := range cand.Cases {
		if b, ok := before[c.Case.ID]; !ok || b != c.Class {
			if !ok {
				b = "absent"
			}
			out = append(out, TriageEvalChange{ID: c.Case.ID, Before: b, After: c.Class})
		}
	}
	return out
}

// `rp project triage eval`: one scorecard per brain revision, failures first, then the pinned
// provenance (brain/cases commits, policy and prompt fingerprints, model) that makes two runs comparable.

package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/digest"
)

// TriageEval renders one report: a verdict line, every non-passing case with expected vs decided and
// the decider's reason, a one-line pass list, the provenance block and what the eval does not cover.
func TriageEval(w io.Writer, rep client.TriageEvalReport, s digest.TriageEvalScore) {
	triageEvalSummary(w, rep, s)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	var passed []string
	failing := false
	for _, c := range s.Cases {
		if c.Class == digest.TriagePass {
			passed = append(passed, c.Case.ID)
			continue
		}
		if !failing {
			_, _ = fmt.Fprintln(tw, "\nCLASS\tCASE\tEXPECTED\tDECIDED\tBY\tREASON")
			failing = true
		}
		why := client.TriageEvalAttempt{Reason: "no attempts"}
		if c.Evidence >= 0 {
			why = c.Case.Attempts[c.Evidence]
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", c.Class, c.Case.ID, triageExpect(c.Case.Expect),
			triageDecided(c), why.DecisionSource, truncate(oneLine(why.Reason), 90))
	}
	_ = tw.Flush()
	if len(passed) > 0 {
		_, _ = fmt.Fprintf(w, "\npass: %s\n", strings.Join(passed, ", "))
	}
	triageEvalProvenance(w, rep)
}

// TriageEvalCompare renders a baseline and a candidate over the same answer key: both verdict lines,
// the cases whose class moved, then the candidate in full.
func TriageEvalCompare(w io.Writer, base, cand client.TriageEvalReport, bs, cs digest.TriageEvalScore) {
	_, _ = fmt.Fprint(w, "baseline  ")
	triageEvalSummary(w, base, bs)
	_, _ = fmt.Fprint(w, "candidate ")
	triageEvalSummary(w, cand, cs)
	if base.PromptHash == cand.PromptHash {
		_, _ = fmt.Fprintln(w, "note: identical prompt fingerprint, so the candidate changed nothing the classifier sees")
	}
	changes := digest.CompareTriageEval(bs, cs)
	if len(changes) == 0 {
		_, _ = fmt.Fprintln(w, "\nno case changed class")
	} else {
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(tw, "\nCASE\tBASELINE\tCANDIDATE")
		for _, c := range changes {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\n", c.ID, c.Before, c.After)
		}
		_ = tw.Flush()
	}
	_, _ = fmt.Fprintln(w, "\n— candidate —")
	TriageEval(w, cand, cs)
}

func triageEvalSummary(w io.Writer, rep client.TriageEvalReport, s digest.TriageEvalScore) {
	total := len(s.Cases)
	_, _ = fmt.Fprintf(w, "%s @ %s: %d/%d pass · %d false skip · %d extra process · %d flaky · %d failed open",
		rep.Brain.Ref, shortSHA(rep.Brain.SHA), s.Pass, total, s.FalseSkip, s.ExtraProcess, s.Flaky, s.FailOpen)
	if rep.Repeat > 1 {
		_, _ = fmt.Fprintf(w, " · ×%d each", rep.Repeat)
	}
	if s.Proposed > 0 {
		_, _ = fmt.Fprintf(w, " · %d/%d labels proposed (not owner-approved)", s.Proposed, total)
	}
	_, _ = fmt.Fprintln(w)
}

func triageEvalProvenance(w io.Writer, rep client.TriageEvalReport) {
	p := rep.Policy
	scope := p.Scope
	if p.Tenant != "" {
		scope += " " + p.Tenant
	}
	if p.Mailbox != "" {
		scope += " mailbox " + clipID(p.Mailbox, 8)
	}
	guidance := make([]string, 0, len(p.Guidance))
	for _, g := range p.Guidance {
		guidance = append(guidance, g.Scope+":"+shortSHA(g.Hash))
	}
	_, _ = fmt.Fprintf(w, "\nbrain    %s @ %s · triage docs: %s\n", rep.Brain.Ref, rep.Brain.SHA, orDash(strings.Join(p.BrainFiles, ", "), "none"))
	_, _ = fmt.Fprintf(w, "cases    %s @ %s · %s · %d cases · hash %s\n", rep.Cases.Ref, shortSHA(rep.Cases.SHA), rep.Cases.Dir, rep.Cases.Count, shortSHA(rep.Cases.Hash))
	_, _ = fmt.Fprintf(w, "policy   %s scope · guidance %s · %d rules %s · language %s · email-type gate %s\n",
		scope, orDash(strings.Join(guidance, " "), "none"), p.RuleCount, shortSHA(p.RulesHash), truncate(p.Language, 40), onOff(p.EmailTypesGate))
	model := rep.Model.Name
	if rep.Model.Fallback != "" {
		model += " (fallback " + rep.Model.Fallback + ")"
	}
	_, _ = fmt.Fprintf(w, "model    %s · route %s · prompt %s · host %s\n", model, rep.Model.Fingerprint, shortSHA(rep.PromptHash), orDash(rep.HostBuild, "dev"))
	for _, n := range rep.NotCovered {
		_, _ = fmt.Fprintf(w, "not covered: %s\n", n)
	}
}

func triageExpect(e client.TriageEvalExpect) string {
	if e.Process == nil || !*e.Process {
		return "skip"
	}
	var why []string
	if e.NeedsAction != nil && *e.NeedsAction {
		why = append(why, "action")
	}
	if e.NeedsPatientText != nil && *e.NeedsPatientText {
		why = append(why, "text")
	}
	return "process (" + strings.Join(why, "+") + ")"
}

func triageDecided(c digest.TriageCaseScore) string {
	n := len(c.Case.Attempts)
	switch {
	case n == 1 && c.Processed == 1:
		return "process"
	case n == 1:
		return "skip"
	case c.Processed == 0:
		return fmt.Sprintf("skip ×%d", n)
	case c.Processed == n:
		return fmt.Sprintf("process ×%d", n)
	default:
		return fmt.Sprintf("process %d/%d", c.Processed, n)
	}
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

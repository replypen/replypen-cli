package debugdump

import (
	"fmt"
	"strings"

	"github.com/replypen/replypen-cli/internal/client"
)

// PhoneLabIndex keeps event positions, since tool rows are replaced in place and timestamps need not ascend.
func PhoneLabIndex(r *client.PhoneLabRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Phone lab %s · %s · %s\n\n", r.RunID, cell(r.Project, 80), r.Status)
	fmt.Fprintf(&b, "Tenant: %s · voice: %s · backend: %s\n\nBrain: %s\n\nStarted: %s", cell(r.Tenant, 80), cell(r.Voice, 80), cell(r.Backend, 80), cell(r.BrainRef, 120), r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	if r.EndedAt != nil {
		fmt.Fprintf(&b, " · duration: %s", r.EndedAt.Sub(r.CreatedAt).Round(1e6))
	}
	fmt.Fprintf(&b, "\n\nCaller lookup: %s %s\n\n## Signals\n\n", cell(r.Caller.Status, 80), cell(r.Caller.Reason, 120))
	flags := 0
	if r.Caller.Status == "lookup_failed" {
		flags++
		b.WriteString("- Caller lookup failed; inspect .caller.error in JSON.\n")
	}
	pendingCaller := false
	for i, ev := range r.Events {
		reasons := []string{}
		if ev.Status == "stale" || ev.Status == "retired" || ev.Status == "failed" || ev.Status == "dropped" || ev.Status == "abandoned" {
			reasons = append(reasons, ev.Kind+" "+ev.Status)
		}
		if ev.ExitCode != nil && *ev.ExitCode != 0 {
			reasons = append(reasons, fmt.Sprintf("bash exit %d", *ev.ExitCode))
		}
		if ev.Kind == "voicemail" {
			reasons = append(reasons, "voicemail "+ev.Status+" "+ev.Outcome)
		}
		if ev.Kind == "note" {
			reasons = append(reasons, ev.Text)
		}
		if ev.Kind == "tool" && (ev.Outcome != "" && ev.Outcome != "ok" && ev.Outcome != "preflight_ok" && ev.Outcome != "would_execute" || ev.Status == "started") {
			reasons = append(reasons, "tool "+ev.Status+" "+ev.Outcome)
		}
		if ev.Kind == "transcript" {
			if ev.Who == "caller" {
				pendingCaller = true
			}
			if ev.Who == "agent" {
				pendingCaller = false
				if ev.LatencyMS != nil && *ev.LatencyMS >= 5000 {
					reasons = append(reasons, fmt.Sprintf("turn transcript latency %d ms (not measured audio silence)", *ev.LatencyMS))
				}
			}
		}
		if len(reasons) > 0 {
			flags++
			fmt.Fprintf(&b, "- [%d] +%.3fs: %s\n", i, float64(ev.T)/1000, cell(strings.Join(reasons, "; "), 200))
		}
	}
	if pendingCaller && r.EndedAt != nil {
		flags++
		fmt.Fprintf(&b, "- Last caller transcript has no later agent transcript.\n")
	}
	if flags == 0 {
		b.WriteString("- No automatic signals; inspect the transcript and final notes.\n")
	}
	b.WriteString("\n## Timeline\n\nTimes and latency refer to transcript events, not audible speech. Tool timestamps mark start. Full stdout/stderr/args/results remain in JSON.\n\n| JSON index | t (s) | kind / who / status | latency / exec | detail |\n|---|---:|---|---|---|\n")
	for i, ev := range r.Events {
		timing := ""
		if ev.LatencyMS != nil {
			timing += fmt.Sprintf("turn %d ms ", *ev.LatencyMS)
		}
		if ev.ExecMS != nil {
			timing += fmt.Sprintf("exec %d ms", *ev.ExecMS)
		}
		detail := ev.Text
		if ev.Cmd != "" {
			detail = ev.Cmd
		}
		if ev.Name != "" {
			detail = ev.Name + " " + detail
		}
		if ev.Delegation != "" {
			detail = "delegation=" + ev.Delegation + " " + detail
		}
		if ev.ExitCode != nil {
			detail += fmt.Sprintf(" exit=%d", *ev.ExitCode)
		}
		if ev.Outcome != "" {
			detail += " outcome=" + ev.Outcome
		}
		fmt.Fprintf(&b, "| %d | %.3f | %s | %s | %s |\n", i, float64(ev.T)/1000, cell(ev.Kind+" "+ev.Who+" "+ev.Status, 90), timing, cell(detail, 140))
	}
	b.WriteString("\nRead one full event: `jq '.events[INDEX]' <trace.json>`. End reasons are final note events; voicemail closure reasons are in `args`. Stored event/byte caps and tool-output truncation also apply to this dump.\n")
	if r.ConsoleRunID != "" {
		fmt.Fprintf(&b, "\nWorkspace context: `rp run debug %s --project %s` (separate console run).\n", r.ConsoleRunID, r.Project)
	}
	return b.String()
}

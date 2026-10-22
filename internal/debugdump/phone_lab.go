package debugdump

import (
	"fmt"
	"strings"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/digest"
)

// PhoneLabIndex keeps event positions, since tool rows are replaced in place and timestamps need not ascend.
func PhoneLabIndex(r *client.PhoneLabRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Phone lab %s · %s · %s\n\n", r.RunID, cell(r.Project, 80), r.Status)
	fmt.Fprintf(&b, "Tenant: %s · voice: %s · backend: %s\n\nBrain: %s\n\nStarted: %s", cell(r.Tenant, 80), cell(r.Voice, 80), cell(r.Backend, 80), cell(r.BrainRef, 120), r.CreatedAt.Format("2006-01-02T15:04:05Z07:00"))
	if r.EndedAt != nil {
		fmt.Fprintf(&b, " · duration: %s", r.EndedAt.Sub(r.CreatedAt).Round(1e6))
	}
	fmt.Fprintf(&b, "\n\nCaller lookup: %s %s\n\n## Anomalies (heuristics)\n\n", cell(r.Caller.Status, 80), cell(r.Caller.Reason, 120))
	if r.Caller.Status == "lookup_failed" {
		b.WriteString("- Caller lookup failed; inspect .caller.error in JSON.\n")
	}
	signals := digest.PhoneLabSignals(r)
	for _, signal := range signals {
		fmt.Fprintf(&b, "- [%d] +%.3fs: %s\n", signal.Index, float64(r.Events[signal.Index].T)/1000, cell(signal.Text, 200))
	}
	if len(signals) == 0 && r.Caller.Status != "lookup_failed" {
		b.WriteString("- No automatic signals; inspect the transcript and final notes.\n")
	}
	b.WriteString("\n## Turns\n\nTranscript deltas, in stored order. Latency is host-reported transcript latency; gaps do not prove audible silence.\n\n| Event index | t (s) | who | latency (ms) | transcript |\n|---|---:|---|---:|---|\n")
	for i, ev := range r.Events {
		if ev.Kind != "transcript" {
			continue
		}
		latency := ""
		if ev.LatencyMS != nil {
			latency = fmt.Sprint(*ev.LatencyMS)
		}
		fmt.Fprintf(&b, "| %d | %.3f | %s | %s | %s |\n", i, float64(ev.T)/1000, cell(ev.Who, 20), latency, cell(ev.Text, 500))
	}
	b.WriteString("\n## Timeline\n\nTimes and latency refer to transcript events, not audible speech. Tool timestamps mark start. Full stdout/stderr/args/results remain in JSON and event JSONL.\n\n| JSON index | t (s) | kind / who / status | latency / exec | detail |\n|---|---:|---|---|---|\n")
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
			if ev.Kind == "model" {
				detail = fmt.Sprintf("seq=%d %s", ev.Seq, detail)
			}
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
		// A tool command keeps its flags: a 140-rune cap hid `book.py … --email` and read as a missing argument.
		limit := 140
		if ev.Cmd != "" {
			limit = 1000
		}
		fmt.Fprintf(&b, "| %d | %.3f | %s | %s | %s |\n", i, float64(ev.T)/1000, cell(ev.Kind+" "+ev.Who+" "+ev.Status, 90), timing, cell(detail, limit))
	}
	b.WriteString("\nJSONL: one `type=phone_run` header (`run` object), then `type=event` records with original `index` and full `event`. Filter: `jq 'select(.type==\"event\" and .event.kind==\"model\")' <trace.jsonl>`.\n\nRead one full event: `jq '.events[INDEX]' <trace.json>`. End reasons are final note events; voicemail closure reasons are in `args`. Stored event/byte caps and tool-output truncation also apply to this dump.\n")
	if r.ConsoleRunID != "" {
		fmt.Fprintf(&b, "\nWorkspace context: `rp run debug %s --project %s` (separate console run).\n", r.ConsoleRunID, r.Project)
	}
	return b.String()
}

package digest

import (
	"fmt"
	"sort"
	"strings"

	"github.com/replypen/replypen-cli/internal/client"
)

type PhoneLabSignal struct {
	Index int
	Text  string
}

// PhoneLabSignals reports stored-evidence heuristics; transcript gaps are never audio measurements.
func PhoneLabSignals(r *client.PhoneLabRun) []PhoneLabSignal {
	var signals []PhoneLabSignal
	add := func(i int, text string) { signals = append(signals, PhoneLabSignal{i, text}) }
	order := make([]int, len(r.Events))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return r.Events[order[i]].T < r.Events[order[j]].T })
	end := int64(0)
	for _, ev := range r.Events {
		if ev.T > end {
			end = ev.T
		}
	}
	if r.EndedAt != nil && !r.CreatedAt.IsZero() {
		end = max(end, r.EndedAt.Sub(r.CreatedAt).Milliseconds())
	}
	pendingCaller := -1
	for _, i := range order {
		ev := r.Events[i]
		if ev.Status == "stale" || ev.Status == "retired" || ev.Status == "failed" || ev.Status == "dropped" || ev.Status == "abandoned" {
			add(i, ev.Kind+" "+ev.Status)
		}
		if ev.ExitCode != nil && *ev.ExitCode != 0 {
			add(i, fmt.Sprintf("bash exit %d", *ev.ExitCode))
		}
		if ev.Kind == "voicemail" && ev.Status == "done" && ev.Outcome != "created" && ev.Outcome != "transcribing" && ev.Outcome != "saved" {
			add(i, "voicemail "+ev.Status+" "+ev.Outcome)
		}
		if ev.Kind == "tool" {
			if ev.ExecMS != nil && *ev.ExecMS > 2000 {
				add(i, fmt.Sprintf("slow tool: %d ms (>2000 ms)", *ev.ExecMS))
			}
			if ev.Status == "started" || ev.Outcome != "" && ev.Outcome != "ok" && ev.Outcome != "preflight_ok" && ev.Outcome != "would_execute" {
				add(i, "tool "+ev.Status+" "+ev.Outcome)
			}
		}
		if ev.Kind != "transcript" {
			continue
		}
		if ev.Who == "caller" {
			pendingCaller = i
			if phoneContains(ev.Text, "êtes-vous là", "êtes-vous toujours là", "êtes toujours là", "vous êtes là", "are you there", "still there", "bent u er nog", "ben je er nog") {
				add(i, "caller checks whether agent is still there")
			}
		}
		if ev.Who != "agent" {
			continue
		}
		pendingCaller = -1
		if ev.LatencyMS != nil && *ev.LatencyMS >= 5000 {
			add(i, fmt.Sprintf("turn transcript latency %d ms (not measured audio silence)", *ev.LatencyMS))
		}
		if !phoneContains(ev.Text, "un instant", "je regarde", "je vérifie", "one moment", "let me check", "een ogenblik", "ik kijk", "ik zoek") {
			continue
		}
		if phoneLookupCovered(r.Events, ev.T) {
			continue
		}
		if end-ev.T >= 3000 {
			add(i, "lookup filler without delegation within 3000 ms (heuristic)")
		}
	}
	if pendingCaller >= 0 && r.EndedAt != nil {
		add(pendingCaller, "Last caller transcript has no later agent transcript.")
	}
	return signals
}

func phoneContains(text string, needles ...string) bool {
	text = strings.NewReplacer("’", "'", "ê", "e", "é", "e", "è", "e").Replace(strings.ToLower(text))
	for _, needle := range needles {
		needle = strings.NewReplacer("ê", "e", "é", "e", "è", "e").Replace(needle)
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func phoneLookupCovered(events []client.PhoneLabEvent, t int64) bool {
	for _, ev := range events {
		if ev.Kind != "delegation" || ev.Status != "created" {
			continue
		}
		if ev.T > t {
			if ev.T-t <= 3000 {
				return true
			}
			continue
		}
		// A queued/in-flight lookup can legitimately emit more than one filler.
		active := true
		for _, result := range events {
			if result.Kind == "commentary" && result.Delegation == ev.Delegation && result.T >= ev.T && result.T <= t {
				active = false
				break
			}
		}
		if active {
			return true
		}
	}
	return false
}

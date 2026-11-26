package digest

import (
	"encoding/json"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/replypen/replypen-cli/internal/client"
)

// PhoneCall is the review view of one phone-lab run: what was said and what the call changed. It reads only
// the run's stored events; outcomes come from the brain scripts' own JSON stdout, never from the transcript.
type PhoneCall struct {
	RunID     string
	StartedAt time.Time
	Duration  time.Duration // 0 while the run has no end
	Caller    string        // masked: "***991"
	Outcomes  []PhoneOutcome
	Lines     []PhoneLine
	Turns     int // caller lines
}

// PhoneOutcome is one completed state change: Kind = booked|moved|cancelled|message|waitlist, Label = the
// script's own human label ("dinsdag 8 december om 11 uur") when it printed one.
type PhoneOutcome struct {
	Kind  string `json:"kind"`
	Label string `json:"label,omitempty"`
}

// PhoneLine is one spoken line (Who = agent|caller) or, with steps, one script call (Who = "step").
type PhoneLine struct {
	T      time.Duration
	Who    string
	Text   string
	Status string // step only: the script's stdout status, or "exit N" / the tool outcome when it failed
	Failed bool
}

// Outcome is the one-word list cell: the outcomes joined, "hung up" when the caller never spoke, "no action"
// for a conversation that changed nothing.
func (c PhoneCall) Outcome() string {
	if len(c.Outcomes) > 0 {
		kinds := make([]string, 0, len(c.Outcomes))
		for _, o := range c.Outcomes {
			kinds = append(kinds, o.Kind)
		}
		return strings.Join(kinds, "+")
	}
	if c.Turns == 0 {
		return "hung up"
	}
	return "no action"
}

// phoneScriptOutcome maps a brain phone script to the state change it makes. Keyed on the verb the scripts are
// named after, so every phone brain (nl, be-nl, fr) reads the same.
var phoneScriptOutcome = map[string]string{
	"book": "booked", "move": "moved", "cancel": "cancelled", "message": "message", "voicemail": "message", "waitlist": "waitlist",
}

// phoneDoneStatus: stdout statuses that mean the write happened (an idempotent retry counts too). Previews
// ("confirm"), refusals and errors are not outcomes.
var phoneDoneStatus = map[string]bool{
	"booked": true, "moved": true, "cancelled": true, "noted": true, "done": true, "changed": true, "saved": true,
	"already_booked": true, "already_noted": true, "already_saved": true, "already_reserved": true,
}

// DigestPhoneCall builds the review view. steps adds one "step" line per script call.
func DigestPhoneCall(r *client.PhoneLabRun, steps bool) PhoneCall {
	c := PhoneCall{RunID: r.RunID, StartedAt: r.CreatedAt, Caller: MaskPhone(r.Caller.Phone)}
	if r.EndedAt != nil && !r.CreatedAt.IsZero() {
		c.Duration = r.EndedAt.Sub(r.CreatedAt).Round(time.Second)
	}
	events := append([]client.PhoneLabEvent(nil), r.Events...)
	sort.SliceStable(events, func(i, j int) bool { return events[i].T < events[j].T })
	seen := map[string]bool{}
	for _, ev := range events {
		at := time.Duration(ev.T) * time.Millisecond
		switch ev.Kind {
		case "transcript":
			text := strings.TrimSpace(ev.Text)
			if text == "" || ev.Who != "agent" && ev.Who != "caller" {
				continue
			}
			if ev.Who == "caller" {
				c.Turns++
			}
			c.Lines = append(c.Lines, PhoneLine{T: at, Who: ev.Who, Text: text})
		case "tool":
			script, args := phoneScript(ev.Cmd)
			if script == "" || ev.Status == "started" {
				continue
			}
			out := phoneStdout(ev.Stdout)
			failed := ev.ExitCode != nil && *ev.ExitCode != 0 || ev.Outcome != "" && ev.Outcome != "ok"
			if kind, ok := phoneScriptOutcome[script]; ok && !failed && phoneDoneStatus[out.Status] {
				key := kind + "|" + out.Label
				if !seen[key] {
					seen[key] = true
					c.Outcomes = append(c.Outcomes, PhoneOutcome{Kind: kind, Label: out.Label})
				}
			}
			if steps {
				status := out.Status
				switch {
				case ev.ExitCode != nil && *ev.ExitCode != 0:
					status = "exit " + strconv.Itoa(*ev.ExitCode)
				case ev.Outcome != "" && ev.Outcome != "ok":
					status = ev.Outcome
				}
				c.Lines = append(c.Lines, PhoneLine{T: at, Who: "step", Text: strings.TrimSpace(script + " " + args), Status: status, Failed: failed})
			}
		case "voicemail":
			// A voicemail message left on the line (DentAI transcribes it); the run event carries no label.
			if ev.Status == "done" && !seen["message|"] {
				seen["message|"] = true
				c.Outcomes = append(c.Outcomes, PhoneOutcome{Kind: "message"})
			}
		}
	}
	return c
}

type phoneOut struct {
	Status string `json:"status"`
	Label  string `json:"label"`
}

func phoneStdout(s string) phoneOut {
	var out phoneOut
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 { // a script that logs first prints its JSON last
		s = s[i+1:]
	}
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

// phoneScript returns a brain script's verb ("book" for .../scripts/book.py) and its arguments, or "" for a
// command that is not a `python <script>.py` call.
func phoneScript(cmd string) (string, string) {
	fields := strings.Fields(cmd)
	if len(fields) < 2 || !strings.HasPrefix(path.Base(fields[0]), "python") || !strings.HasSuffix(fields[1], ".py") {
		return "", ""
	}
	return strings.TrimSuffix(path.Base(fields[1]), ".py"), strings.Join(fields[2:], " ")
}

// MaskPhone keeps the last three digits ("***991"), the convention of the host's own phone notes.
func MaskPhone(phone string) string {
	var digits []byte
	for i := 0; i < len(phone); i++ {
		if phone[i] >= '0' && phone[i] <= '9' {
			digits = append(digits, phone[i])
		}
	}
	switch {
	case len(digits) == 0:
		return "—"
	case len(digits) <= 3:
		return "***"
	}
	return "***" + string(digits[len(digits)-3:])
}

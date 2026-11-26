package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/replypen/replypen-cli/internal/digest"
)

// PhoneCalls is `rp run calls`: one line per call, newest first, times in loc.
func PhoneCalls(w io.Writer, calls []digest.PhoneCall, project string, loc *time.Location) {
	if len(calls) == 0 {
		_, _ = fmt.Fprintln(w, "No calls.")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "WHEN\tCALLER\tLENGTH\tTURNS\tOUTCOME\tRUN")
	for _, c := range calls {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\n", c.StartedAt.In(loc).Format("Mon 02 Jan 15:04"), c.Caller, callLength(c.Duration), c.Turns, c.Outcome(), clipID(c.RunID, 8))
	}
	_ = tw.Flush()
	_, _ = fmt.Fprintf(w, "\nRead one: rp run call <run> --project %s   (--steps: script calls · -o json: raw run)\n", project)
}

// PhoneCall is `rp run call <run>`: a header, the dialogue (plus script steps when the digest carries them)
// and the outcome.
func PhoneCall(w io.Writer, c digest.PhoneCall, project, tenant string, steps bool, loc *time.Location) {
	where := project
	if tenant != "" {
		where += "/" + tenant
	}
	_, _ = fmt.Fprintf(w, "Call %s · %s · caller %s · %s · %s\n\n", clipID(c.RunID, 8), c.StartedAt.In(loc).Format("Mon 02 Jan 2006 15:04 MST"), c.Caller, callLength(c.Duration), where)
	if len(c.Lines) == 0 {
		_, _ = fmt.Fprintln(w, "(no transcript)")
	}
	for _, l := range c.Lines {
		at := fmt.Sprintf("%02d:%02d", int(l.T.Minutes()), int(l.T.Seconds())%60)
		switch l.Who {
		case "step":
			mark := "→"
			if l.Failed {
				mark = "✗"
			}
			_, _ = fmt.Fprintf(w, "%s          ⚙ %s %s %s\n", at, truncate(l.Text, 110), mark, orDash(l.Status, "?"))
		default:
			who := "Agent "
			if l.Who == "caller" {
				who = "Caller"
			}
			_, _ = fmt.Fprintf(w, "%s  %s  %s\n", at, who, wrapHanging(l.Text, 84, strings.Repeat(" ", 15)))
		}
	}
	_, _ = fmt.Fprintf(w, "\nOutcome: %s\n", phoneOutcome(c))
	if !steps {
		_, _ = fmt.Fprintf(w, "More: --steps (script calls) · -o json (raw run) · rp run debug %s --phone-lab --project %s\n", c.RunID, project)
	}
}

func phoneOutcome(c digest.PhoneCall) string {
	if len(c.Outcomes) == 0 {
		return c.Outcome()
	}
	parts := make([]string, 0, len(c.Outcomes))
	for _, o := range c.Outcomes {
		if o.Label != "" {
			parts = append(parts, o.Kind+" "+o.Label)
		} else {
			parts = append(parts, o.Kind)
		}
	}
	return strings.Join(parts, " · ")
}

func callLength(d time.Duration) string {
	if d <= 0 {
		return "live"
	}
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

// wrapHanging word-wraps a turn so long agent lines stay in the text column instead of folding under the time.
func wrapHanging(s string, width int, indent string) string {
	var b strings.Builder
	n := 0
	for _, word := range strings.Fields(s) {
		size := len([]rune(word))
		if n > 0 && n+1+size > width {
			b.WriteString("\n" + indent)
			n = 0
		} else if n > 0 {
			b.WriteByte(' ')
			n++
		}
		b.WriteString(word)
		n += size
	}
	return b.String()
}

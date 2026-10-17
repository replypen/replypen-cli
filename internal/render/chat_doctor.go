package render

import (
	"fmt"
	"io"
	"time"

	"github.com/replypen/replypen-cli/internal/digest"
)

// ChatDoctor prints the `rp project chat doctor` table: one line per finding in check order, then one per
// reject marked recent or stale against the --since window.
func ChatDoctor(w io.Writer, r digest.ChatDoctorReport) {
	for _, f := range r.Findings {
		_, _ = fmt.Fprintf(w, "%-7s %-28s %s\n", f.Status, f.Check, f.Hint)
	}
	for _, reject := range r.Rejects {
		state := "recent"
		if reject.Stale {
			state = "stale"
		}
		_, _ = fmt.Fprintf(w, "%-7s %-28s %s\n", state, reject.Code, reject.Timestamp.Format(time.RFC3339))
	}
}

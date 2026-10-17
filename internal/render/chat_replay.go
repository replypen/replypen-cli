package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// replayAnswerChars bounds the table's answer cell: enough to recognise the answer, the full text is
// in `-o json` or `rp run trace <run_id> --brief`.
const replayAnswerChars = 80

// ChatTurn is one sent chat turn (`rp project chat send`/`replay`); the `-o json` row of replay.
type ChatTurn struct {
	Turn      int     `json:"turn,omitempty"`
	Message   string  `json:"message,omitempty"`
	SessionID string  `json:"session_id"`
	RunID     string  `json:"run_id,omitempty"`
	Tier      string  `json:"tier,omitempty"`
	Secs      float64 `json:"secs"`
	Answer    string  `json:"answer,omitempty"`
	Error     string  `json:"error,omitempty"`
}

// ChatReplay prints the turn table, then each session id and every tier change between
// consecutive turns of one session.
func ChatReplay(w io.Writer, turns []ChatTurn) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "#\tTIER\tSECS\tRUN_ID\tANSWER")
	for _, t := range turns {
		answer := t.Answer
		if t.Error != "" {
			answer = "error: " + t.Error
		}
		_, _ = fmt.Fprintf(tw, "%d\t%s\t%.1f\t%s\t%s\n", t.Turn, dash(t.Tier), t.Secs, dash(t.RunID), answerHead(answer))
	}
	_ = tw.Flush()
	var prev *ChatTurn
	var switches []string
	for i := range turns {
		t := &turns[i]
		if prev == nil || prev.SessionID != t.SessionID {
			_, _ = fmt.Fprintf(w, "session_id: %s\n", t.SessionID)
		} else if prev.Tier != "" && t.Tier != "" && prev.Tier != t.Tier {
			switches = append(switches, fmt.Sprintf("switched at turn %d (%s → %s)", t.Turn, prev.Tier, t.Tier))
		}
		prev = t
	}
	for _, s := range switches {
		_, _ = fmt.Fprintln(w, s)
	}
}

func answerHead(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > replayAnswerChars {
		s = string(r[:replayAnswerChars-1]) + "…"
	}
	if s == "" {
		return "-"
	}
	return s
}

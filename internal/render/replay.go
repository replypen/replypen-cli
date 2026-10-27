package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/replypen/replypen-cli/internal/client"
)

// replayDiffMaxLines caps the printed draft diff: a rewrite of the whole draft is one glance, not a page.
const replayDiffMaxLines = 40

// ReplayCompare renders the tail of `rp ask --replay-run`: where the original is, and how the replay's
// answer and proposed actions differ from it. nil orig (not readable) prints nothing.
func ReplayCompare(w io.Writer, orig, replay *client.RunDetail) {
	if orig == nil || replay == nil {
		return
	}
	link := orig.RunURL
	if link == "" {
		link = orig.RunID
	}
	_, _ = fmt.Fprintf(w, "\nCompared with the original run %s:\n", link)
	before, after := replayAnswer(orig), replayAnswer(replay)
	if before == after {
		_, _ = fmt.Fprintln(w, "Draft: identical")
	} else {
		_, _ = fmt.Fprintln(w, "Draft (- original, + replay):")
		lines := lineDiff(strings.Split(before, "\n"), strings.Split(after, "\n"))
		for i, l := range lines {
			if i == replayDiffMaxLines {
				_, _ = fmt.Fprintf(w, "  … %d more changed lines\n", len(lines)-i)
				break
			}
			_, _ = fmt.Fprintf(w, "  %s\n", l)
		}
	}
	a, b := actionSlugs(orig.ProposedActions), actionSlugs(replay.ProposedActions)
	if a == b {
		_, _ = fmt.Fprintf(w, "Proposed actions: identical (%s)\n", a)
	} else {
		_, _ = fmt.Fprintf(w, "Proposed actions: %s → %s\n", a, b)
	}
}

func replayAnswer(r *client.RunDetail) string {
	if s := strings.TrimSpace(r.DraftMarkdown); s != "" {
		return s
	}
	return strings.TrimSpace(r.AnswerMarkdown)
}

func actionSlugs(actions []client.ProposedAction) string {
	if len(actions) == 0 {
		return "none"
	}
	out := make([]string, 0, len(actions))
	for _, a := range actions {
		name := a.Slug
		if name == "" {
			name = a.Label
		}
		out = append(out, name)
	}
	return strings.Join(out, ", ")
}

// lineDiff returns only the changed lines of a, b ("- x" removed, "+ y" added), in order, via an LCS table.
// Drafts are a few dozen lines, so the quadratic table is fine.
func lineDiff(a, b []string) []string {
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []string
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			i, j = i+1, j+1
		case j < len(b) && (i == len(a) || lcs[i][j+1] >= lcs[i+1][j]):
			out = append(out, "+ "+b[j])
			j++
		default:
			out = append(out, "- "+a[i])
			i++
		}
	}
	return out
}

package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/replypen/replypen-cli/internal/client"
)

// ReviewCards is the `rp project review ls` table: one row per open card.
func ReviewCards(w io.Writer, deck client.ReviewDeck) {
	if len(deck.Items) == 0 {
		_, _ = fmt.Fprintf(w, "(no open %s review cards)\n", orDash(deck.Audience, "owner"))
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "ID\tSTATUS\tSEVERITY\tDECISION\tSUBJECT")
	for _, card := range deck.Items {
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", card.ID, card.Status, orDash(card.Severity, "-"),
			truncate(orDash(card.Label, "-"), 24), truncate(card.Subject, 70))
	}
	_ = tw.Flush()
}

// ReviewCard is the `rp project review show` view of one card, ending on its review-page link.
func ReviewCard(w io.Writer, card client.ReviewCard, link string) {
	_, _ = fmt.Fprintln(w, card.Subject)
	_, _ = fmt.Fprintf(w, "ID:        %s\nStatus:    %s\n", card.ID, card.Status)
	if card.Severity != "" || card.Kind != "" {
		_, _ = fmt.Fprintf(w, "Severity:  %s · %s\n", orDash(card.Severity, "-"), orDash(card.Kind, "-"))
	}
	if card.FirstSeen != "" {
		_, _ = fmt.Fprintf(w, "Since:     %s\n", card.FirstSeen)
	}
	if card.Label != "" {
		_, _ = fmt.Fprintf(w, "Decision:  %s\n", card.Label)
		if card.Instruction != "" {
			_, _ = fmt.Fprintf(w, "           %s\n", strings.ReplaceAll(card.Instruction, "\n", "\n           "))
		}
	}
	if len(card.Options) > 0 {
		labels := make([]string, 0, len(card.Options))
		for _, o := range card.Options {
			labels = append(labels, o.Label)
		}
		_, _ = fmt.Fprintf(w, "Options:   %s (or Other, Skip)\n", strings.Join(labels, " | "))
	}
	if card.Receipt != "" {
		_, _ = fmt.Fprintf(w, "Receipt:   %s\n", strings.ReplaceAll(card.Receipt, "\n", "\n           "))
	}
	_, _ = fmt.Fprintf(w, "Link:      %s\n", link)
}

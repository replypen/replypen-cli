package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/client"
)

// newDraftRenderCmd builds `rp dev tools draft-render <file|->...`: the host's own markdown→HTML for a
// mail draft, so a local correction rewrites the text/html leg exactly as ReplyPen placed it.
func newDraftRenderCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "draft-render <markdown-file|->...",
		Short: "Render draft markdown to the HTML ReplyPen places in a mail draft",
		Long: "Render 1-4 draft markdown files (- = stdin) with the host's placement renderer, the HTML a " +
			"Gmail/Outlook/IMAP draft gets before any font, signature or quoted original. -o json prints " +
			"{\"html\":[...]} in input order; otherwise the HTML of each input, one per block.",
		Args: cobra.RangeArgs(1, 4),
		RunE: func(_ *cobra.Command, args []string) error {
			req := client.DraftRenderRequest{}
			for _, arg := range args {
				var b []byte
				var err error
				if arg == "-" {
					in := e.in
					if in == nil {
						in = os.Stdin
					}
					b, err = io.ReadAll(in)
				} else {
					b, err = os.ReadFile(arg)
				}
				if err != nil {
					return fmt.Errorf("read %s: %w", arg, err)
				}
				req.Markdown = append(req.Markdown, string(b))
			}
			c, err := e.newClient()
			if err != nil {
				return err
			}
			out, raw, err := c.DraftRender(e.ctx(), e.scopeProject(), req)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				return e.renderJSONInline(raw)
			}
			for _, html := range out.HTML {
				_, _ = fmt.Fprintln(e.out, html)
			}
			return nil
		},
	}
}

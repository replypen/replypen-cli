package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/render"
)

// newSiteCmd builds `rp project site domain add|status|rm <host>`: put a ReplyPen-hosted website on its tenant's
// own hostname. The tenant adds one CNAME at its own DNS host; rerun `add` until it reports live.
func newSiteCmd(e *env) *cobra.Command {
	site := &cobra.Command{Use: "site", Short: "ReplyPen-hosted websites (--tenant for a tenant's site)"}
	domain := &cobra.Command{
		Use:   "domain",
		Short: "Connect a website to its own hostname (www.example.be): add, status, rm",
		Long: "add connects the hostname and prints the one DNS record the domain owner adds; rerun add until it\n" +
			"reports live: true, which also moves ReplyPen's live address to it (status only reads). Project admins\n" +
			"only. The bare domain cannot point here: forward it to the www name at the registrar. rm disconnects it.",
	}
	var mirror string
	for _, v := range []struct{ verb, short string }{
		{"add", "Connect (idempotent: rerun = advance + status)"},
		{"status", "Show where the hostname stands (changes nothing)"},
		{"rm", "Disconnect the hostname"},
	} {
		verb := v.verb
		domain.AddCommand(&cobra.Command{
			Use:   verb + " <host>",
			Short: v.short,
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				c, err := e.newClient()
				if err != nil {
					return err
				}
				if err := e.resolvePinnedProject(c); err != nil {
					return err
				}
				raw, err := c.SiteDomain(e.ctx(), verb, e.scopeProject(), e.scopeTenant(), strings.TrimSpace(args[0]), mirror)
				if err != nil {
					return err
				}
				if e.jsonOut() {
					return e.renderJSONInline(raw)
				}
				render.Item(e.out, asItem(raw))
				return nil
			},
		})
	}
	domain.PersistentFlags().StringVar(&mirror, "mirror", "site", "website mirror name")
	site.AddCommand(domain)
	return site
}

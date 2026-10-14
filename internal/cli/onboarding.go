package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rootcause-org/rootcause-cli/internal/client"
)

// newDevOnboardingCmd drives a local `make dev` server's onboarding worlds (cmd/devserver/onboarding.go).
func newDevOnboardingCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "onboarding", Short: "Drive onboarding worlds on a local make dev server"}
	var step, world, devURL string
	gotoCmd := &cobra.Command{
		Use:   "goto",
		Short: "Re-seed an onboarding world at a step and print the URL to open",
		Long: "Re-seeds the world's project (every earlier step confirmed, pulls settled from fixtures) on the\n" +
			"make dev server and prints its URL. Worlds: northwind (default), website-failed, website-unreachable,\n" +
			"website-partial, personal, empty-inbox, few-sent, sorting-failed (the server lists the current set\n" +
			"on an unknown world). The dev url comes from --url, else ROOTCAUSE_BASE_URL.",
		Example: "  rc dev onboarding goto --step 4\n  rc dev onboarding goto --step email_types --world sorting-failed --url http://localhost:24729",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if devURL == "" {
				devURL = os.Getenv("ROOTCAUSE_BASE_URL")
			}
			if devURL == "" {
				return fmt.Errorf("no dev url: pass --url http://localhost:<port> (make dev prints it) or set ROOTCAUSE_BASE_URL")
			}
			out, err := client.DevOnboardingGoto(e.ctx(), devURL, step, world)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				return writeJSON(e, out)
			}
			_, err = fmt.Fprintf(e.out, "%s (%s)\nopen:       %s\nplayground: %s\n", out.Label, out.Project, out.URL, out.Playground)
			return err
		},
	}
	gotoCmd.Flags().StringVar(&step, "step", "", "step number 1-6 or key (inbox, website, tools, tone_profile, email_types, draft_example)")
	gotoCmd.Flags().StringVar(&world, "world", "northwind", "fixture world")
	gotoCmd.Flags().StringVar(&devURL, "url", "", "make dev url (default ROOTCAUSE_BASE_URL)")
	_ = gotoCmd.MarkFlagRequired("step")
	cmd.AddCommand(gotoCmd)
	return cmd
}

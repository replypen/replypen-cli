package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// adminBrainRemoteCmd moves a project's brains on or off GitHub through the server, which holds the
// brain writers' locks while it verifies and removes the remote.
func adminBrainRemoteCmd(e *env) *cobra.Command {
	var mode string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "brain-remote <project> --mode none|github [--dry-run]",
		Short: "Move a project's brains off GitHub (none) or back (github) (human global admin)",
		Long: "Leaving GitHub (--mode none) refuses unless, for the project brain and every tenant brain, main/stable/edge " +
			"equal GitHub, nothing exists only on GitHub, no run is in flight and the off-box backup holds the current refs; " +
			"then it removes the remote. Nothing on GitHub is deleted. --mode github re-adds the remote and pushes. " +
			"--dry-run runs the checklist only.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := e.newClient()
			if err != nil {
				return err
			}
			rep, raw, err := c.AdminBrainRemote(e.ctx(), args[0], mode, dryRun)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				return e.renderJSONInline(raw)
			}
			from := rep.From
			if from == "" {
				from = "(undeclared)"
			}
			_, _ = fmt.Fprintf(e.out, "project %s: %s → %s\n", rep.Project, from, rep.To)
			for _, b := range rep.Brains {
				_, _ = fmt.Fprintf(e.out, "  - %s\n", b)
			}
			for _, s := range rep.Steps {
				_, _ = fmt.Fprintf(e.out, "  ✓ %s\n", s)
			}
			if rep.DryRun {
				_, _ = fmt.Fprintln(e.out, "checklist passed; dry run, nothing changed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "", "none (local-only, no GitHub) or github")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "run the checklist only; change nothing")
	_ = cmd.MarkFlagRequired("mode")
	return cmd
}

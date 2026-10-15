package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rootcause-org/rootcause-cli/internal/client"
	"github.com/rootcause-org/rootcause-cli/internal/render"
)

// newTenantBudgetCmd: project admins read/change a tenant's recurring monthly AI limit. The server is the
// boundary (project-level admin on every project sharing the cap); the CLI only shapes the request.
func newTenantBudgetCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "budget",
		Short: "List, create or change tenants' monthly AI limits in USD (project admins)",
		Long: "The limit is recurring: it applies from now on, every month, to every tenant listed under\n" +
			"\"shared by\". This month's usage is never reset. `set` on a tenant without a limit creates one,\n" +
			"shared with the same tenant in sibling projects. Removing a limit stays with ReplyPen support.",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "ls",
			Short: "List every tenant's monthly AI limit and this month's usage",
			Args:  cobra.NoArgs,
			RunE: func(_ *cobra.Command, _ []string) error {
				c, err := e.newClient()
				if err != nil {
					return err
				}
				project, err := hierarchyProject(e, c)
				if err != nil {
					return err
				}
				b, raw, err := c.ListProjectBudgets(e.ctx(), project)
				if err != nil {
					return err
				}
				if e.jsonOut() {
					return e.renderJSON("tenant-budgets-"+project, raw)
				}
				render.TenantBudgets(e.out, b)
				return nil
			},
		},
		&cobra.Command{
			Use:   "get <slug>",
			Short: "Show a tenant's monthly AI limit and this month's usage",
			Args:  cobra.ExactArgs(1),
			RunE: func(_ *cobra.Command, args []string) error {
				return tenantBudgetRun(e, args[0], nil)
			},
		},
		tenantBudgetWriteCmd(e, "set", "Set (or create) a tenant's monthly AI limit as an absolute USD amount", false),
		tenantBudgetWriteCmd(e, "add", "Raise (or with a negative amount, lower) a tenant's monthly AI limit by USD", true),
	)
	return cmd
}

func tenantBudgetWriteCmd(e *env, verb, short string, delta bool) *cobra.Command {
	var usd float64
	cmd := &cobra.Command{
		Use:   verb + " <slug> --usd <amount>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("usd") {
				return fmt.Errorf("--usd <amount> is required")
			}
			patch := client.TenantBudgetPatch{MonthlyUSD: &usd}
			if delta {
				patch = client.TenantBudgetPatch{AddUSD: &usd}
			}
			return tenantBudgetRun(e, args[0], &patch)
		},
	}
	cmd.Flags().Float64Var(&usd, "usd", 0, "amount in USD, at most 2 decimals")
	return cmd
}

func tenantBudgetRun(e *env, slug string, patch *client.TenantBudgetPatch) error {
	c, err := e.newClient()
	if err != nil {
		return err
	}
	project, err := hierarchyProject(e, c)
	if err != nil {
		return err
	}
	var l *client.TenantBudgetLimit
	var raw []byte
	if patch == nil {
		l, raw, err = c.GetTenantBudgetLimit(e.ctx(), project, slug)
	} else {
		l, raw, err = c.PatchTenantBudgetLimit(e.ctx(), project, slug, *patch)
	}
	if err != nil {
		return err
	}
	if e.jsonOut() {
		return e.renderJSON("tenant-budget-"+slug, raw)
	}
	render.TenantBudget(e.out, project+"/"+slug, l)
	return nil
}

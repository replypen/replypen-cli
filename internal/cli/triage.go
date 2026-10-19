package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/digest"
	"github.com/replypen/replypen-cli/internal/render"
)

func newTriageCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "triage",
		Short: "Read or change mail triage policy and hard rules",
	}
	cmd.AddCommand(triagePolicyCmd(e), triageRulesCmd(e), triageEvalCmd(e))
	return cmd
}

func triagePolicyCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "policy", Short: "Read or edit free-form triage guidance"}
	cmd.AddCommand(
		triagePolicyGetCmd(e),
		triagePolicySetCmd(e),
	)
	return cmd
}

func triagePolicyGetCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show triage policy guidance",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			raw, err := triageCall(e, func(c *client.Client) (json.RawMessage, error) {
				return c.TriagePolicy(e.ctx(), e.scopeProject(), e.scopeTenant())
			})
			if err != nil {
				return err
			}
			return e.renderJSONInline(raw)
		},
	}
}

func triagePolicySetCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "set <guidance>",
		Short: "Replace triage policy guidance",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			raw, err := triageCall(e, func(c *client.Client) (json.RawMessage, error) {
				return c.SetTriagePolicy(e.ctx(), e.scopeProject(), e.scopeTenant(), args[0])
			})
			if err != nil {
				return err
			}
			return e.renderJSONInline(raw)
		},
	}
}

func triageRulesCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "rules", Short: "Read or edit deterministic triage rules"}
	cmd.AddCommand(
		triageRulesListCmd(e),
		triageRuleAddCmd(e),
		triageRuleSetCmd(e),
		triageRuleRmCmd(e),
	)
	return cmd
}

func triageRulesListCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List triage hard rules",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			raw, err := triageCall(e, func(c *client.Client) (json.RawMessage, error) {
				return c.TriageRules(e.ctx(), e.scopeProject(), e.scopeTenant())
			})
			if err != nil {
				return err
			}
			return e.renderJSONInline(raw)
		},
	}
}

func triageRuleAddCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "add effect=<skip|force_process> match_kind=<...> pattern=<...> [header_name=...] [reason=...] [priority=N] [enabled=true|false]",
		Short: "Create a triage hard rule",
		Args:  cobra.MinimumNArgs(3),
		RunE: func(_ *cobra.Command, args []string) error {
			body, err := parseTriageRuleArgs(args)
			if err != nil {
				return err
			}
			raw, err := triageCall(e, func(c *client.Client) (json.RawMessage, error) {
				return c.CreateTriageRule(e.ctx(), e.scopeProject(), e.scopeTenant(), body)
			})
			if err != nil {
				return err
			}
			return e.renderJSONInline(raw)
		},
	}
}

func triageRuleSetCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "set <id> field=value [field=value...]",
		Short: "Patch a triage hard rule",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			body, err := parseTriageRuleArgs(args[1:])
			if err != nil {
				return err
			}
			raw, err := triageCall(e, func(c *client.Client) (json.RawMessage, error) {
				return c.PatchTriageRule(e.ctx(), e.scopeProject(), e.scopeTenant(), args[0], body)
			})
			if err != nil {
				return err
			}
			return e.renderJSONInline(raw)
		},
	}
}

func triageRuleRmCmd(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "rm <id>",
		Short: "Delete a triage hard rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			raw, err := triageCall(e, func(c *client.Client) (json.RawMessage, error) {
				return c.DeleteTriageRule(e.ctx(), e.scopeProject(), e.scopeTenant(), args[0])
			})
			if err != nil {
				return err
			}
			if e.jsonOut() {
				if len(raw) > 0 {
					return e.renderJSONInline(raw)
				}
				return e.renderJSONInline([]byte(`{"deleted":"` + args[0] + `"}`))
			}
			_, _ = fmt.Fprintf(e.out, "deleted triage rule %s\n", args[0])
			return nil
		},
	}
}

// triageCall is the one client hop every triage subcommand shares: build the client, then let the
// caller name the endpoint. Path, verb and the project/tenant scoping rule live in internal/client.
func triageCall(e *env, call func(*client.Client) (json.RawMessage, error)) (json.RawMessage, error) {
	c, err := e.newClient()
	if err != nil {
		return nil, err
	}
	return call(c)
}

func parseTriageRuleArgs(args []string) (map[string]any, error) {
	body := make(map[string]any, len(args))
	for _, arg := range args {
		key, val, ok := strings.Cut(arg, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("invalid argument %q: expected key=value", arg)
		}
		switch key {
		case "effect", "match_kind", "pattern", "header_name", "reason":
			body[key] = val
		case "enabled":
			b, err := strconv.ParseBool(val)
			if err != nil {
				return nil, fmt.Errorf("enabled: %q is not a boolean (use true/false)", val)
			}
			body[key] = b
		case "priority":
			n, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("priority: %q is not an integer", val)
			}
			body[key] = n
		default:
			return nil, fmt.Errorf("unknown triage rule field %q", key)
		}
	}
	return body, nil
}

func triageEvalCmd(e *env) *cobra.Command {
	var req client.TriageEvalRequest
	var baseline, only string
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Run the brain's triage case corpus through the real triage decision at a brain ref",
		Long: "Reads every case in the brain's _internal/triage/cases/ at --cases-ref (default HEAD, the synced main), " +
			"classifies each through the server's real triage path (prefilter, live policy + rules, the triage docs " +
			"of --brain-ref, persona language, email-type gate, model route) and scores it against the case's " +
			"expected outcome. Nothing is ingested, drafted or sent; it spends one model call per case and --repeat. " +
			"--baseline <ref> runs that ref first and grades both on the baseline's exact cases commit. " +
			"It does not measure the main agent (active-session follow-ups skip triage).",
		Example: "  rp project triage eval --brain-ref dev/shorter-triage --baseline HEAD --repeat 2",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, err := e.newClient()
			if err != nil {
				return err
			}
			if only != "" {
				for _, id := range strings.Split(only, ",") {
					if id = strings.TrimSpace(id); id != "" {
						req.Only = append(req.Only, id)
					}
				}
			}
			project, tenant := e.scopeProject(), e.scopeTenant()
			if project == "" {
				project = e.resolved.Project
			}
			if baseline == "" {
				rep, raw, err := c.TriageEval(e.ctx(), project, tenant, req)
				if err != nil {
					return err
				}
				if e.jsonOut() {
					return e.renderJSON("triage-eval", raw)
				}
				render.TriageEval(e.out, rep, digest.ScoreTriageEval(rep))
				return nil
			}
			baseReq := req
			baseReq.BrainRef = baseline
			base, baseRaw, err := c.TriageEval(e.ctx(), project, tenant, baseReq)
			if err != nil {
				return fmt.Errorf("baseline %s: %w", baseline, err)
			}
			// Pin the candidate to the baseline's exact answer key, even if --cases-ref moves meanwhile.
			req.CasesRef = base.Cases.SHA
			cand, candRaw, err := c.TriageEval(e.ctx(), project, tenant, req)
			if err != nil {
				return fmt.Errorf("candidate %s: %w", orHEAD(req.BrainRef), err)
			}
			if e.jsonOut() {
				both, _ := json.Marshal(map[string]json.RawMessage{"baseline": baseRaw, "candidate": candRaw})
				return e.renderJSON("triage-eval", both)
			}
			render.TriageEvalCompare(e.out, base, cand, digest.ScoreTriageEval(base), digest.ScoreTriageEval(cand))
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&req.BrainRef, "brain-ref", "", "brain revision whose triage docs to test: HEAD (default), stable, edge, a commit SHA or a dev/* branch")
	f.StringVar(&req.CasesRef, "cases-ref", "", "brain revision to read the cases (answer key) from (default HEAD)")
	f.StringVar(&baseline, "baseline", "", "also run this brain ref first and show which cases changed class")
	f.StringVar(&req.Mailbox, "mailbox", "", "mailbox id: adds its mailbox-scoped policy, rules and persona language")
	f.StringVar(&only, "only", "", "comma-separated case ids to run")
	f.IntVar(&req.Repeat, "repeat", 1, "decisions per case (1-3): a case passes only when every attempt agrees")
	return cmd
}

func orHEAD(ref string) string {
	if ref == "" {
		return "HEAD"
	}
	return ref
}

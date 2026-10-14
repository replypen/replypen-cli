package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rootcause-org/rootcause-cli/internal/client"
	"github.com/rootcause-org/rootcause-cli/internal/digest"
	"github.com/rootcause-org/rootcause-cli/internal/render"
)

// tokenLsCmd: GET /api/v1/tokens with client-side last-used review. Default shows active tokens only,
// least-recently-used first; --unused-days N keeps the never-used / stale ones (the cleanup list).
// -o json keeps the server envelope but applies the same filter + sort to its items. --all swaps the
// source for the box-admin fleet view GET /api/v1/admin/tokens (every project, with owner); the server
// owns its revoked window, so --include-revoked is forwarded rather than filtered here.
func tokenLsCmd(e *env) *cobra.Command {
	var unusedDays int
	var includeRevoked, wide, fleet bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List tokens with last-used info (active only, least recently used first)",
		Long: "List tokens, least recently used first. Active tokens only unless --include-revoked. \"never\" means no use\n" +
			"since last-used tracking began (2026-10-12). Use --unused-days N to list stale credentials worth revoking.\n" +
			"-o json keeps the server's raw items (scope, user agent, …) after the same filter and sort.\n" +
			"--all lists machine credentials across EVERY project with their owner (all-projects global-admin token);\n" +
			"with --include-revoked it adds revoked, expired and superseded ones that died in the last 30 days.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if unusedDays < 0 {
				return fmt.Errorf("--unused-days must be >= 0")
			}
			c, err := e.newClient()
			if err != nil {
				return err
			}
			var l *client.ListResponse
			var raw json.RawMessage
			if fleet {
				l, raw, err = c.AdminTokens(e.ctx(), includeRevoked)
			} else {
				l, raw, err = c.List(e.ctx(), "tokens", e.scopeProject(), collectionTenant(e, "tokens"))
			}
			if err != nil {
				return err
			}
			items := digest.SelectTokens(l.Items, digest.TokenFilter{IncludeInactive: includeRevoked, UnusedDays: unusedDays, Now: nowFunc()})
			if e.jsonOut() {
				out, err := replaceListItems(raw, items)
				if err != nil {
					return err
				}
				return e.renderJSON("tokens-ls", out)
			}
			if fleet {
				render.TokenFleetList(e.out, items, nowFunc(), wide)
			} else {
				render.TokenList(e.out, items, nowFunc(), wide)
			}
			return nil
		},
	}
	cmd.Flags().IntVar(&unusedDays, "unused-days", 0, "only tokens never used or last used more than N days ago")
	cmd.Flags().BoolVar(&includeRevoked, "include-revoked", false, "include revoked tokens (default: active only; with --all: revoked/expired/superseded in the last 30 days)")
	cmd.Flags().BoolVar(&wide, "wide", false, "add SCOPE and USER AGENT columns (with --all: ID and the full user agent)")
	cmd.Flags().BoolVar(&fleet, "all", false, "machine credentials across every project, with owner (all-projects global-admin token)")
	return cmd
}

// replaceListItems swaps the item array inside a list body (bare array, or a resource-keyed envelope
// like {"tokens":[…]}) for items, leaving other envelope keys untouched — the JSON twin of
// client.ListResponse.UnmarshalJSON.
func replaceListItems(raw json.RawMessage, items []client.Item) (json.RawMessage, error) {
	if items == nil {
		items = []client.Item{}
	}
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) == nil {
		return json.Marshal(items)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	key := ""
	if v, ok := obj["tokens"]; ok && isItemArray(v) {
		key = "tokens"
	}
	for k, v := range obj {
		if key == "" && isItemArray(v) {
			key = k
		}
	}
	if key == "" {
		return raw, nil
	}
	b, err := json.Marshal(items)
	if err != nil {
		return nil, err
	}
	obj[key] = b
	return json.Marshal(obj)
}

func isItemArray(v json.RawMessage) bool {
	var probe []client.Item
	return len(v) > 0 && v[0] == '[' && json.Unmarshal(v, &probe) == nil
}

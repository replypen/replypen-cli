package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// TenantBudgetLimit is GET|PATCH /api/v1/projects/{p}/tenants/{t}/ai-budget/limit: the project-admin view
// of a tenant's recurring monthly AI limit. Field names match the server's tenantbudget.Limit.
type TenantBudgetLimit struct {
	Metered     bool     `json:"metered"`
	Account     string   `json:"account,omitempty"`
	MonthlyUSD  *float64 `json:"monthly_usd"`
	UsedPercent *int     `json:"used_percent"`
	Paused      bool     `json:"paused"`
	ResetsAt    string   `json:"resets_at"`
	Tenants     []string `json:"tenants"`
}

// TenantBudgetPatch is exactly one of MonthlyUSD (absolute) or AddUSD (signed delta).
type TenantBudgetPatch struct {
	MonthlyUSD *float64 `json:"monthly_usd,omitempty"`
	AddUSD     *float64 `json:"add_usd,omitempty"`
}

func (c *Client) GetTenantBudgetLimit(ctx context.Context, project, tenant string) (*TenantBudgetLimit, json.RawMessage, error) {
	path, err := projectTreePath(project, tenant, "/ai-budget/limit")
	if err != nil {
		return nil, nil, err
	}
	return fetchBoth[TenantBudgetLimit](ctx, c, http.MethodGet, path, nil)
}

// PatchTenantBudgetLimit is never auto-retried by the transport (non-idempotent add).
func (c *Client) PatchTenantBudgetLimit(ctx context.Context, project, tenant string, patch TenantBudgetPatch) (*TenantBudgetLimit, json.RawMessage, error) {
	path, err := projectTreePath(project, tenant, "/ai-budget/limit")
	if err != nil {
		return nil, nil, err
	}
	return fetchBoth[TenantBudgetLimit](ctx, c, http.MethodPatch, path, patch)
}

// ProjectBudgets is GET /api/v1/projects/{p}/ai-budgets: every non-archived tenant with its limit.
type ProjectBudgets struct {
	Tenants []TenantBudgetRow `json:"tenants"`
}

// TenantBudgetRow is one ProjectBudgets entry (the server embeds the limit fields beside tenant).
type TenantBudgetRow struct {
	Tenant string `json:"tenant"`
	TenantBudgetLimit
}

func (c *Client) ListProjectBudgets(ctx context.Context, project string) (*ProjectBudgets, json.RawMessage, error) {
	path, err := projectTreePath(project, "", "/ai-budgets")
	if err != nil {
		return nil, nil, err
	}
	return fetchBoth[ProjectBudgets](ctx, c, http.MethodGet, path, nil)
}

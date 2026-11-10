package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

// RunUsage is one run's summed LLM usage from GET /api/v1/admin/projects/{p}/run-usage (human box admin only).
type RunUsage struct {
	RunID              string  `json:"run_id"`
	Calls              int64   `json:"calls"`
	CostUSD            float64 `json:"cost_usd"`
	PromptTokens       int64   `json:"prompt_tokens"`
	CachedPromptTokens int64   `json:"cached_prompt_tokens"`
	CompletionTokens   int64   `json:"completion_tokens"`
	ReasoningTokens    int64   `json:"reasoning_tokens"`
	TotalTokens        int64   `json:"total_tokens"`
}

// RunUsageList is the {"items":[…]} envelope; a run without usage rows is absent.
type RunUsageList struct {
	Items []RunUsage `json:"items"`
}

// AdminRunUsage reads summed cost/tokens for up to 200 runs of one project.
func (c *Client) AdminRunUsage(ctx context.Context, project string, runIDs []string) (*RunUsageList, json.RawMessage, error) {
	path := "/api/v1/admin/projects/" + url.PathEscape(project) + "/run-usage?" + url.Values{"ids": {strings.Join(runIDs, ",")}}.Encode()
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, nil, err
	}
	var out RunUsageList
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, raw, err
	}
	return &out, raw, nil
}

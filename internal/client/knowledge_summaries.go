package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// KnowledgeSummaryPatch is one reviewed article summary, pinned to the body_hash it was judged against.
type KnowledgeSummaryPatch struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	BodyHash string `json:"body_hash"`
	Summary  string `json:"summary"`
}

type KnowledgeSummariesRequest struct {
	Patches []KnowledgeSummaryPatch `json:"patches"`
	DryRun  bool                    `json:"dry_run"`
}

// KnowledgeSummaryCheck is the server's verdict per patch: ok | stale | not_found | invalid.
type KnowledgeSummaryCheck struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	Status   string `json:"status"`
	Reason   string `json:"reason,omitempty"`
}

type KnowledgeSummariesResponse struct {
	DryRun  bool                    `json:"dry_run"`
	Results []KnowledgeSummaryCheck `json:"results"`
	Queued  int                     `json:"queued"`
	JobID   string                  `json:"job_id,omitempty"`
}

// KnowledgeSummariesApply checks reviewed summaries against the scope's KB and (unless dry-run) queues
// the KB sync that applies the current ones. The tenant twin targets the tenant's own KB.
func (c *Client) KnowledgeSummariesApply(ctx context.Context, project, tenant string, req KnowledgeSummariesRequest) (*KnowledgeSummariesResponse, json.RawMessage, error) {
	if err := requireTenantProject(project, tenant, "knowledge summaries"); err != nil {
		return nil, nil, err
	}
	if project == "" {
		return nil, nil, &APIError{Status: http.StatusBadRequest, Code: "PROJECT_REQUIRED", Message: "knowledge summaries require a project scope"}
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, treePath(project, tenant, "/knowledge/summaries"), req, &raw); err != nil {
		return nil, nil, err
	}
	var out KnowledgeSummariesResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, raw, err
	}
	return &out, raw, nil
}

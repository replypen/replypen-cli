package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// KnowledgeSyncStatus is one on-demand KB sync job of a scope (POST …/knowledge/sync and
// GET …/knowledge/sync/{job}). HealthState is the scope's kb_health verdict: a job completes even when a
// source failed, so only it says whether the help centre was read. Changed is set only with ?since=.
type KnowledgeSyncStatus struct {
	Project      string   `json:"project"`
	Tenant       string   `json:"tenant,omitempty"`
	JobID        int64    `json:"job_id"`
	State        string   `json:"state"`
	Done         bool     `json:"done"`
	Deduped      bool     `json:"deduped,omitempty"`
	Error        string   `json:"error,omitempty"`
	KBCommit     string   `json:"kb_commit,omitempty"`
	HealthState  string   `json:"health_state,omitempty"`
	ArticleTotal int      `json:"article_total"`
	Changed      []string `json:"changed,omitempty"`
}

// KnowledgeSyncNow queues a KB sync of the scope (tenant → that tenant's own KB).
func (c *Client) KnowledgeSyncNow(ctx context.Context, project, tenant string) (*KnowledgeSyncStatus, json.RawMessage, error) {
	return c.knowledgeSync(ctx, http.MethodPost, project, tenant, "/knowledge/sync")
}

// KnowledgeSyncJob reads one sync job; since (a kb_commit) adds the article files changed after it.
func (c *Client) KnowledgeSyncJob(ctx context.Context, project, tenant string, jobID int64, since string) (*KnowledgeSyncStatus, json.RawMessage, error) {
	suffix := fmt.Sprintf("/knowledge/sync/%d", jobID)
	if since != "" {
		suffix += "?since=" + url.QueryEscape(since)
	}
	return c.knowledgeSync(ctx, http.MethodGet, project, tenant, suffix)
}

func (c *Client) knowledgeSync(ctx context.Context, method, project, tenant, suffix string) (*KnowledgeSyncStatus, json.RawMessage, error) {
	if err := requireTenantProject(project, tenant, "knowledge sync"); err != nil {
		return nil, nil, err
	}
	if project == "" {
		return nil, nil, &APIError{Status: http.StatusBadRequest, Code: "PROJECT_REQUIRED", Message: "knowledge sync requires a project scope"}
	}
	var raw json.RawMessage
	if err := c.do(ctx, method, treePath(project, tenant, suffix), nil, &raw); err != nil {
		return nil, nil, err
	}
	var out KnowledgeSyncStatus
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, raw, err
	}
	return &out, raw, nil
}

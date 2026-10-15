package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

func repliesPath(project, tenant string) (string, error) {
	if project == "" || tenant == "" {
		return "", fmt.Errorf("--project and --tenant are required: the replies feed is per tenant")
	}
	return treePath(project, tenant, "/replies"), nil
}

// TenantReplies fetches one page of a tenant's answered turns, newest first.
func (c *Client) TenantReplies(ctx context.Context, project, tenant string, limit int, cursor string) (*TenantReplyList, json.RawMessage, error) {
	path, err := repliesPath(project, tenant)
	if err != nil {
		return nil, nil, err
	}
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, nil, err
	}
	var out TenantReplyList
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil, err
	}
	return &out, raw, nil
}

// TenantReplyFeedback posts a score and/or comment on one turn's run, fenced to the tenant server-side.
func (c *Client) TenantReplyFeedback(ctx context.Context, project, tenant, runID string, body map[string]any) (json.RawMessage, error) {
	path, err := repliesPath(project, tenant)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, path+"/"+url.PathEscape(runID)+"/feedback", body, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

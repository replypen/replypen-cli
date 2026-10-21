package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type PhoneLabRun struct {
	Caller struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	} `json:"caller"`

	RunID        string          `json:"run_id"`
	Project      string          `json:"project"`
	Tenant       string          `json:"tenant"`
	TenantID     string          `json:"tenant_id"`
	Status       string          `json:"status"`
	Voice        string          `json:"voice"`
	Backend      string          `json:"backend"`
	BrainRef     string          `json:"brain_ref"`
	ConsoleRunID string          `json:"console_run_id"`
	CreatedAt    time.Time       `json:"created_at"`
	EndedAt      *time.Time      `json:"ended_at"`
	Events       []PhoneLabEvent `json:"events"`
}

type PhoneLabList struct {
	Items      []PhoneLabRun `json:"items"`
	NextCursor string        `json:"next_cursor"`
}

func (c *Client) PhoneLabRuns(ctx context.Context, project, tenant string, limit int) (*PhoneLabList, json.RawMessage, error) {
	q := url.Values{}
	if tenant != "" {
		q.Set("tenant", tenant)
	}
	if limit != 0 {
		q.Set("limit", strconv.Itoa(limit))
	}
	path := "/api/v1/projects/" + url.PathEscape(project) + "/phone-lab/runs"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, nil, err
	}
	var out PhoneLabList
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil, err
	}
	return &out, raw, nil
}

type PhoneLabEvent struct {
	Seq        int64  `json:"seq"`
	T          int64  `json:"t"`
	Kind       string `json:"kind"`
	Who        string `json:"who"`
	Text       string `json:"text"`
	LatencyMS  *int64 `json:"latency_ms"`
	Delegation string `json:"delegation"`
	Status     string `json:"status"`
	Name       string `json:"name"`
	Args       string `json:"args"`
	ExecMS     *int64 `json:"exec_ms"`
	Outcome    string `json:"outcome"`
	Cmd        string `json:"cmd"`
	ExitCode   *int   `json:"exit_code"`
}

func (c *Client) PhoneLabRunWithRaw(ctx context.Context, project, id, tenant string) (*PhoneLabRun, json.RawMessage, error) {
	path := "/api/v1/projects/" + url.PathEscape(project) + "/phone-lab/runs/" + url.PathEscape(id)
	if tenant != "" {
		path += "?tenant=" + url.QueryEscape(tenant)
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, nil, err
	}
	var out PhoneLabRun
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil, err
	}
	return &out, raw, nil
}

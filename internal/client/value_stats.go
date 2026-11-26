package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// ValueStatsRow is one (month, tenant, mailbox) row of GET /api/v1/admin/projects/{p}/value-stats.
// continued_* = the thread saw a send after our draft (as proposed, edited, own reply, or auto-sent).
type ValueStatsRow struct {
	Month            string `json:"month"`
	Tenant           string `json:"tenant"`
	MailboxID        string `json:"mailbox_id"`
	Mailbox          string `json:"mailbox"`
	Runs             int64  `json:"runs"`
	DraftedRuns      int64  `json:"drafted_runs"`
	ContinuedRuns    int64  `json:"continued_runs"`
	Provider         string `json:"provider"`
	SentVerbatim     int64  `json:"sent_verbatim"`
	SentMinorEdit    int64  `json:"sent_minor_edit"`
	SentMajorEdit    int64  `json:"sent_major_edit"`
	SentRewritten    int64  `json:"sent_rewritten"`
	AutoSent         int64  `json:"auto_sent"`
	DraftedThreads   int64  `json:"drafted_threads"`
	ContinuedThreads int64  `json:"continued_threads"`
}

// ValueStats is the envelope: the resolved [since, until) window plus rows.
type ValueStats struct {
	Project string          `json:"project"`
	Tenant  string          `json:"tenant"`
	Since   string          `json:"since"`
	Until   string          `json:"until"`
	Items   []ValueStatsRow `json:"items"`
}

// AdminValueStats reads the per-month drafted → continued counts (human box admin only). q carries
// months | since+until and an optional tenant slug.
func (c *Client) AdminValueStats(ctx context.Context, project string, q url.Values) (*ValueStats, json.RawMessage, error) {
	path := "/api/v1/admin/projects/" + url.PathEscape(project) + "/value-stats"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, nil, err
	}
	var out ValueStats
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, raw, err
	}
	return &out, raw, nil
}

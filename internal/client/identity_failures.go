package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

type IdentityFailureList struct {
	Items      []IdentityFailure `json:"items"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

type IdentityFailure struct {
	RunID     string  `json:"run_id"`
	CreatedAt string  `json:"created_at"`
	Tenant    *string `json:"tenant"`
	Category  string  `json:"category"`
}

func (c *Client) AdminIdentityFailures(ctx context.Context, project string, simulation bool, cursor string) (*IdentityFailureList, json.RawMessage, error) {
	path := "/api/v1/admin/projects/" + url.PathEscape(project) + "/identity-failures"
	q := url.Values{}
	if simulation {
		q.Set("simulation", "true")
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
	var out IdentityFailureList
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, raw, err
	}
	return &out, raw, nil
}

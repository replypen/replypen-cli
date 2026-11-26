package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// SearchEvent is one help-center search or click the project's website reported (server:
// internal/api/search_events.go). Q is stored redacted; Sid joins a click or a feedback vote to its search.
type SearchEvent struct {
	Kind    string   `json:"kind"`
	Sid     string   `json:"sid"`
	Q       string   `json:"q"`
	N       *int     `json:"n"`
	Shown   []string `json:"shown"`
	Clicked *string  `json:"clicked"`
	Rank    *int     `json:"rank"`
	Rev     string   `json:"rev"`
	V       *int     `json:"v"`
	Vote    *int     `json:"vote"` // feedback rows: +1 / -1 on the article in Clicked
	At      string   `json:"at"`
}

type SearchEventList struct {
	Project   string        `json:"project"`
	From      string        `json:"from"`
	To        string        `json:"to"`
	Truncated bool          `json:"truncated"`
	Items     []SearchEvent `json:"items"`
}

// SearchEvents reads GET /api/v1/projects/{project}/search-events; from/to are YYYY-MM-DD ("" = server default).
func (c *Client) SearchEvents(ctx context.Context, project, from, to string) (*SearchEventList, json.RawMessage, error) {
	q := url.Values{}
	if from != "" {
		q.Set("from", from)
	}
	if to != "" {
		q.Set("to", to)
	}
	path := "/api/v1/projects/" + url.PathEscape(project) + "/search-events"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, path, nil, &raw); err != nil {
		return nil, nil, err
	}
	var out SearchEventList
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, raw, err
	}
	return &out, raw, nil
}

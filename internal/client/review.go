package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// ReviewDeck mirrors the host's review page deck (replypen internal/web/customer/weekly_review.go
// reviewDeck), trimmed to what the CLI renders; -o json passes the raw body through.
type ReviewDeck struct {
	ID       string       `json:"id"`
	Audience string       `json:"audience"`
	Cadence  string       `json:"cadence"`
	Status   string       `json:"status"`
	BaseURL  string       `json:"base_url"`
	Focus    string       `json:"focus,omitempty"`
	Items    []ReviewCard `json:"items"`
}

// ReviewCard is one deck tile. Receipt is only filled for the technical audience.
type ReviewCard struct {
	ID          string         `json:"id"`
	Status      string         `json:"status"`
	Severity    string         `json:"severity"`
	Kind        string         `json:"kind"`
	Subject     string         `json:"subject"`
	FirstSeen   string         `json:"first_seen"`
	Label       string         `json:"decision_label"`
	Instruction string         `json:"decision_instruction"`
	Options     []ReviewOption `json:"options"`
	Receipt     string         `json:"receipt"`
}

type ReviewOption struct {
	Label       string `json:"label"`
	Instruction string `json:"instruction"`
}

// ReviewItemState is the host's answer to a card write (decide/close).
type ReviewItemState struct {
	ID          string          `json:"id"`
	Status      string          `json:"status"`
	Label       string          `json:"decision_label"`
	Instruction string          `json:"decision_instruction"`
	Applied     json.RawMessage `json:"applied"`
}

// reviewPath builds /api/v1/projects/{project}[/tenants/{slug}]/review[/suffix]?audience=…[&item=…].
func reviewPath(project, tenant, suffix, audience, item string) string {
	p := "/api/v1/projects/" + url.PathEscape(project)
	if tenant != "" {
		p += "/tenants/" + url.PathEscape(tenant)
	}
	q := url.Values{}
	if audience != "" {
		q.Set("audience", audience)
	}
	if item != "" {
		q.Set("item", item)
	}
	if len(q) > 0 {
		return p + "/review" + suffix + "?" + q.Encode()
	}
	return p + "/review" + suffix
}

// ReviewDeck reads the open cards of one audience; item also returns that card when already retired.
func (c *Client) ReviewDeck(ctx context.Context, project, tenant, audience, item string) (*ReviewDeck, json.RawMessage, error) {
	raw, err := c.raw(ctx, http.MethodGet, reviewPath(project, tenant, "", audience, item), nil)
	if err != nil {
		return nil, nil, err
	}
	var deck ReviewDeck
	if err := json.Unmarshal(raw, &deck); err != nil {
		return nil, raw, err
	}
	return &deck, raw, nil
}

// ReviewDecide records a decision exactly like the review page (label = an option, Other or Skip).
func (c *Client) ReviewDecide(ctx context.Context, project, tenant, audience, id, label, instruction string) (*ReviewItemState, json.RawMessage, error) {
	return c.reviewWrite(ctx, reviewPath(project, tenant, "/items/"+url.PathEscape(id), audience, ""),
		map[string]any{"decision_label": label, "decision_instruction": instruction})
}

// ReviewClose retires a queue card already handled elsewhere; the note says where.
func (c *Client) ReviewClose(ctx context.Context, project, tenant, audience, id, note string) (*ReviewItemState, json.RawMessage, error) {
	return c.reviewWrite(ctx, reviewPath(project, tenant, "/items/"+url.PathEscape(id)+"/close", audience, ""), map[string]any{"note": note})
}

// ReviewFinish finishes a session (a weekly review applies; the rolling queue only acknowledges).
func (c *Client) ReviewFinish(ctx context.Context, project, tenant, audience, sessionID string) error {
	_, err := c.raw(ctx, http.MethodPost, reviewPath(project, tenant, "/finish", audience, ""), map[string]any{"session_id": sessionID})
	return err
}

func (c *Client) reviewWrite(ctx context.Context, path string, body map[string]any) (*ReviewItemState, json.RawMessage, error) {
	raw, err := c.raw(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, nil, err
	}
	var state ReviewItemState
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, raw, err
	}
	return &state, raw, nil
}

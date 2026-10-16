package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// FeedbackRow is one rated run as GET /api/v1/feedback projects it. Field names match the server
// verbatim. Score is absent on comment-only rows, Comment below the review-content tier and the
// previews below the project-admin tier.
type FeedbackRow struct {
	RunID           string `json:"run_id"`
	Kind            string `json:"kind"`
	SessionID       string `json:"session_id,omitempty"`
	ThreadID        string `json:"thread_id,omitempty"`
	TenantSlug      string `json:"tenant_slug,omitempty"`
	Surface         string `json:"surface,omitempty"`
	SessionTitle    string `json:"session_title,omitempty"`
	Simulation      bool   `json:"simulation"`
	Score           int    `json:"score,omitempty"`
	Comment         string `json:"comment,omitempty"`
	ScoreSetAt      string `json:"score_set_at,omitempty"`
	CommentSetAt    string `json:"comment_set_at,omitempty"`
	FeedbackAt      string `json:"feedback_at"`
	RunCreatedAt    string `json:"run_created_at"`
	QuestionPreview string `json:"question_preview,omitempty"`
	AnswerPreview   string `json:"answer_preview,omitempty"`
}

type FeedbackResponse struct {
	Feedback   []FeedbackRow `json:"feedback"`
	NextBefore string        `json:"next_before,omitempty"`
}

// FeedbackParams are the query filters for GET /api/v1/feedback. Zero values are omitted so the server
// applies its own defaults (every rated or commented run, limit 50).
type FeedbackParams struct {
	Score   string
	Kind    string
	Surface string
	Days    int
	Since   string
	Until   string
	Limit   int
	Before  string
	Project string
	Tenant  string
}

// Feedback fetches GET /api/v1/feedback — the rated-runs index behind `rc run feedback list`.
func (c *Client) Feedback(ctx context.Context, p FeedbackParams) (*FeedbackResponse, json.RawMessage, error) {
	q := url.Values{}
	for k, v := range map[string]string{
		"score": p.Score, "kind": p.Kind, "surface": p.Surface, "since": p.Since, "until": p.Until,
		"before": p.Before, "project": p.Project, "tenant": p.Tenant,
	} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if p.Days > 0 {
		q.Set("days", fmt.Sprintf("%d", p.Days))
	}
	if p.Limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", p.Limit))
	}
	path := "/api/v1/feedback"
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}
	return fetchBoth[FeedbackResponse](ctx, c, http.MethodGet, path, nil)
}

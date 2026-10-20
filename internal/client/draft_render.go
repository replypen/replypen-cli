package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// DraftRenderRequest is POST /api/v1/projects/{project}/draft-render: up to 4 draft markdown bodies.
type DraftRenderRequest struct {
	Markdown []string `json:"markdown"`
}

// DraftRenderResponse holds, per input, the HTML ReplyPen's placement edge renders (before any font,
// signature or quoted original is added).
type DraftRenderResponse struct {
	HTML []string `json:"html"`
}

func (c *Client) DraftRender(ctx context.Context, project string, req DraftRenderRequest) (*DraftRenderResponse, json.RawMessage, error) {
	if project == "" {
		return nil, nil, &APIError{Status: http.StatusBadRequest, Code: "PROJECT_REQUIRED", Message: "draft render requires a project scope"}
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, treePath(project, "", "/draft-render"), req, &raw); err != nil {
		return nil, nil, err
	}
	var out DraftRenderResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil, err
	}
	return &out, raw, nil
}

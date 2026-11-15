package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// BrainRemoteReport is POST /api/v1/admin/projects/{p}/brain-remote's checklist and outcome.
type BrainRemoteReport struct {
	Project  string   `json:"project"`
	From     string   `json:"from"`
	To       string   `json:"to"`
	DryRun   bool     `json:"dry_run"`
	Applied  bool     `json:"applied"`
	Brains   []string `json:"brains"`
	Problems []string `json:"problems,omitempty"`
	Steps    []string `json:"steps,omitempty"`
}

// AdminBrainRemote switches one project's brains between GitHub ("github") and local-only ("none"),
// in the server process (human global admin). A refusal is a 409 BRAIN_REMOTE_REFUSED APIError whose
// Fields list each failed check.
func (c *Client) AdminBrainRemote(ctx context.Context, project, mode string, dryRun bool) (*BrainRemoteReport, json.RawMessage, error) {
	path := "/api/v1/admin/projects/" + url.PathEscape(project) + "/brain-remote"
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, path, map[string]any{"mode": mode, "dry_run": dryRun}, &raw); err != nil {
		return nil, nil, err
	}
	var out BrainRemoteReport
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, raw, err
	}
	return &out, raw, nil
}

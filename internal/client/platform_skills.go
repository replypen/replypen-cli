package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// PlatformSkill is one entry of GET /api/v1/platform-skills: a skill runs mount at /skills/<name>.
type PlatformSkill struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Scenarios   []string `json:"scenarios,omitempty"`
	Requires    string   `json:"requires,omitempty"`
}

// PlatformSkills is the platform skill library manifest; Digest names the deployed bytes.
type PlatformSkills struct {
	Digest string          `json:"digest"`
	Files  int             `json:"files"`
	Bytes  int64           `json:"bytes"`
	Skills []PlatformSkill `json:"skills"`
}

// PlatformSkillsManifest fetches GET /api/v1/platform-skills.
func (c *Client) PlatformSkillsManifest(ctx context.Context) (*PlatformSkills, json.RawMessage, error) {
	return fetchBoth[PlatformSkills](ctx, c, http.MethodGet, "/api/v1/platform-skills", nil)
}

// PlatformSkillsArchive streams GET /api/v1/platform-skills/archive (tar.gz) to w.
func (c *Client) PlatformSkillsArchive(ctx context.Context, w io.Writer) error {
	return c.Download(ctx, "/api/v1/platform-skills/archive", w)
}

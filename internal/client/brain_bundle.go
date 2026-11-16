package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

// Bundle transport for a brain without a remote (brain_remote "none"): download the brain as a git
// bundle, upload one branch as dev/<branch>, and land it on main with BrainSyncRef.

// BrainRefUploadResponse is POST …/brain/refs/{branch}'s receipt.
type BrainRefUploadResponse struct {
	Project string `json:"project"`
	Tenant  string `json:"tenant,omitempty"`
	Ref     string `json:"ref"`
	SHA     string `json:"sha"`
}

// DownloadBrainBundle streams the brain's bundle (main, channels, tags, uploaded dev/ branches) to w.
func (c *Client) DownloadBrainBundle(ctx context.Context, project, tenant string, w io.Writer) error {
	if err := brainScope(project, tenant); err != nil {
		return err
	}
	return c.Download(ctx, treePath(project, tenant, "/brain/bundle"), w)
}

// MaxBrainBundleUpload is the server's cap on an uploaded bundle; push refuses a bigger one before sending.
const MaxBrainBundleUpload int64 = 200 << 20

// UploadBrainRef streams the bundle file at bundlePath as dev/<branch>; head names the bundle ref to take
// ("" = its only head).
func (c *Client) UploadBrainRef(ctx context.Context, project, tenant, branch, head, bundlePath string) (*BrainRefUploadResponse, json.RawMessage, error) {
	if err := brainScope(project, tenant); err != nil {
		return nil, nil, err
	}
	path := treePath(project, tenant, "/brain/refs/"+url.PathEscape(branch))
	if head != "" {
		path += "?head=" + url.QueryEscape(head)
	}
	data, err := c.fetch(ctx, sendSpec{method: http.MethodPost, path: path, bodyFile: bundlePath, contentType: "application/octet-stream"})
	if err != nil {
		return nil, nil, err
	}
	var out BrainRefUploadResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, nil, err
	}
	return &out, json.RawMessage(data), nil
}

// BrainSyncRef fast-forwards a remote-less brain's main to an uploaded ref ("dev/<branch>" or a full SHA
// on one), behind the server's boot check.
func (c *Client) BrainSyncRef(ctx context.Context, project, tenant, ref string) (*BrainSyncResponse, json.RawMessage, error) {
	if err := brainScope(project, tenant); err != nil {
		return nil, nil, err
	}
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodPost, treePath(project, tenant, "/brain/sync"), map[string]any{"ref": ref}, &raw); err != nil {
		return nil, nil, err
	}
	var out BrainSyncResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil, err
	}
	return &out, raw, nil
}

func brainScope(project, tenant string) error {
	if err := requireTenantProject(project, tenant, "brain"); err != nil {
		return err
	}
	if project == "" {
		return &APIError{Status: http.StatusBadRequest, Code: "PROJECT_REQUIRED", Message: "brain commands require a project scope"}
	}
	return nil
}

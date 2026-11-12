package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// SiteDomain calls the host's website-domain routes (…[/tenants/{slug}]/site/domains). verb: add | status | rm.
// add is idempotent on the host (rerun = advance + status); status changes nothing.
func (c *Client) SiteDomain(ctx context.Context, verb, project, tenant, host, mirror string) (json.RawMessage, error) {
	if project == "" {
		return nil, &APIError{Status: http.StatusBadRequest, Code: "PROJECT_REQUIRED", Message: "--project <project> is required for site domains"}
	}
	base := treePath(project, tenant, "/site/domains")
	q := ""
	if mirror != "" {
		q = "?mirror=" + url.QueryEscape(mirror)
	}
	var raw json.RawMessage
	var err error
	switch verb {
	case "add":
		err = c.do(ctx, http.MethodPost, base, map[string]string{"host": host, "mirror": mirror}, &raw)
	case "rm":
		err = c.do(ctx, http.MethodDelete, base+"/"+url.PathEscape(host)+q, nil, &raw)
	default:
		err = c.do(ctx, http.MethodGet, base+"/"+url.PathEscape(host)+q, nil, &raw)
	}
	return raw, err
}

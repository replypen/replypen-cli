package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// KBBundleProblem is one finding of the host's bundle validator.
type KBBundleProblem struct {
	Level   string `json:"level"`
	Article string `json:"article,omitempty"`
	Message string `json:"message"`
}

// KBBundleCheck is the host's verdict on a site-published KB bundle.
type KBBundleCheck struct {
	OK       bool              `json:"ok"`
	Articles int               `json:"articles"`
	Problems []KBBundleProblem `json:"problems"`
}

// CheckKBBundle posts a bundle to the host's account-less validator (POST /kb-bundle/check): the same
// Go parser the `bundle` KB provider runs, so a pass here means the sync will take it. No token needed.
func CheckKBBundle(ctx context.Context, baseURL string, bundle []byte) (*KBBundleCheck, json.RawMessage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/kb-bundle/check", bytes.NewReader(bundle))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: httpTimeout()}).Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("bundle check: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out KBBundleCheck
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, nil, fmt.Errorf("bundle check: decode: %w", err)
	}
	return &out, raw, nil
}

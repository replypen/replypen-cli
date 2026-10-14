package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
)

// OnboardingGoto is the dev server's answer to a goto: the re-seeded world's project and where to open it.
type OnboardingGoto struct {
	Project    string `json:"project"`
	World      string `json:"world"`
	Step       string `json:"step"`
	Label      string `json:"label"`
	URL        string `json:"url"`
	Playground string `json:"playground"`
}

// DevOnboardingGoto re-seeds an onboarding world at a step on a local `make dev` server
// (POST /dev/onboarding/goto). Dev-only and unauthenticated, so it refuses any non-loopback host.
func DevOnboardingGoto(ctx context.Context, baseURL, step, world string) (OnboardingGoto, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" {
		return OnboardingGoto{}, fmt.Errorf("dev url %q: want http://localhost:<port> (the make dev url)", baseURL)
	}
	if ip := net.ParseIP(u.Hostname()); u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return OnboardingGoto{}, fmt.Errorf("dev url %q is not local: onboarding goto only talks to a make dev server", baseURL)
	}
	body, _ := json.Marshal(map[string]string{"step": step, "world": world})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.JoinPath("/dev/onboarding/goto").String(), bytes.NewReader(body))
	if err != nil {
		return OnboardingGoto{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return OnboardingGoto{}, fmt.Errorf("dev server unreachable at %s: %w", baseURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return OnboardingGoto{}, fmt.Errorf("onboarding goto: %s", e.Error)
	}
	var out OnboardingGoto
	if err := json.Unmarshal(raw, &out); err != nil {
		return OnboardingGoto{}, fmt.Errorf("decode goto: %w", err)
	}
	return out, nil
}

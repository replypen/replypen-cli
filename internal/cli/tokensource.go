package cli

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/replypen/replypen-cli/internal/config"
	"github.com/replypen/replypen-cli/internal/oauth"
	"github.com/replypen/replypen-cli/internal/token"
)

// refreshSkew refreshes an access token this long before it actually expires, so a request never races
// the expiry boundary (and the 401-retry path stays a rare backstop, not the common case).
const refreshSkew = 60 * time.Second

// liveSource is the production client.TokenSource: it reads the profile's token from the store, refreshes
// it via OAuth when it's near expiry (or on a forced 401 retry), and PERSISTS the rotated pair so the
// next command starts fresh. All refresh policy lives here — the client stays oblivious to OAuth.
type liveSource struct {
	profile string
	key     string // token-store key: profile namespaced by server (config.StoreKey)
	baseURL string
	oauth   *oauth.Client
	mu      sync.Mutex // serializes this source's Token/Refresh calls
	last    string     // access token last handed out; a 401 on it means "renew unless a sibling already did"
}

// newLiveSource builds the token source for a profile against the issuer at baseURL.
func newLiveSource(profile, baseURL string) *liveSource {
	return &liveSource{profile: profile, key: config.StoreKey(baseURL, profile), baseURL: baseURL, oauth: oauth.NewClient(baseURL)}
}

// Token returns a valid access token, refreshing pre-emptively when it's within refreshSkew of expiry.
func (s *liveSource) Token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok, err := token.Load(s.key)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", s.reauthError()
	}
	if err := s.checkIssuer(t); err != nil {
		return "", err
	}
	if !t.Expired(time.Now(), refreshSkew) {
		s.last = t.AccessToken
		return t.AccessToken, nil
	}
	return s.refresh(ctx, false)
}

// Refresh forces a rotation after the server rejected the access token mid-flight (a 401).
func (s *liveSource) Refresh(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refresh(ctx, true)
}

// refresh renews the access token inside the profile's cross-process lock (token.Update). Must hold s.mu.
//
// Parallel `rp` processes on one profile all see the same expiring token, and the server's rotating
// refresh token is single-use: an unserialized second refresh gets invalid_grant even though the login
// is healthy. So check → refresh → persist is one per-profile token.Update critical section (other
// profiles refresh in parallel), and after acquiring the lock we re-read: a sibling that renewed while
// we waited leaves a fresh token we simply adopt.
// force (a 401 retry) skips the refresh only when the stored token differs from the rejected one.
func (s *liveSource) refresh(ctx context.Context, force bool) (string, error) {
	var out, presented string
	err := token.Update(s.key, func(cur token.Token, ok bool) (token.Token, bool, error) {
		if !ok {
			return cur, false, s.reauthError()
		}
		if err := s.checkIssuer(cur); err != nil {
			return cur, false, err
		}
		renewed := !force || (s.last != "" && cur.AccessToken != s.last)
		if renewed && !cur.Expired(time.Now(), refreshSkew) {
			out = cur.AccessToken
			return cur, false, nil
		}
		if cur.RefreshToken == "" {
			return cur, false, s.reauthError()
		}
		presented = cur.RefreshToken
		res, err := s.oauth.Refresh(ctx, cur.RefreshToken)
		if err != nil {
			return cur, false, fmt.Errorf("refresh access token: %w", err)
		}
		next := token.Token{
			AccessToken:     res.AccessToken,
			RefreshToken:    cur.RefreshToken, // a non-rotating grant returns none → keep the one we have
			ExpiresAt:       time.Now().Add(time.Duration(res.ExpiresIn) * time.Second),
			BaseURL:         s.baseURL,
			MachineTokenEnv: cur.MachineTokenEnv,
		}
		if res.RefreshToken != "" {
			next.RefreshToken = res.RefreshToken // rotating grant: store the new refresh
		}
		out = next.AccessToken
		return next, true, nil
	})
	if err == nil {
		s.last = out
		return out, nil
	}
	if !oauth.IsInvalidGrant(err) {
		return "", err
	}
	// invalid_grant: re-read once before declaring the session dead — a sibling outside our lock (an older
	// rp) may have rotated the refresh token we presented.
	if fresh, ok, lerr := token.Load(s.key); lerr == nil && ok && sameServer(fresh.BaseURL, s.baseURL) &&
		fresh.RefreshToken != presented && !fresh.Expired(time.Now(), refreshSkew) {
		s.last = fresh.AccessToken
		return fresh.AccessToken, nil
	}
	return "", s.reauthError()
}

// checkIssuer refuses a stored token minted by another server: neither its access token nor its refresh
// token may ever be presented here (e.g. a prod "default" entry overwritten by a staging login before
// the store was namespaced).
func (s *liveSource) checkIssuer(t token.Token) error {
	if sameServer(t.BaseURL, s.baseURL) {
		return nil
	}
	return authenticationError(fmt.Sprintf("profile %q holds a token from %s, not %s — run `%s`",
		s.profile, t.BaseURL, s.baseURL, loginCommand(s.baseURL)))
}

// reauthError is the shared "session can't be refreshed — log in again" error.
func (s *liveSource) reauthError() error {
	return authenticationError(fmt.Sprintf("session expired (profile %q%s) — run `%s`", s.profile, onServer(s.baseURL), loginCommand(s.baseURL)))
}

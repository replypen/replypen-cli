package cli

import (
	"fmt"

	"github.com/replypen/replypen-cli/internal/config"
	"github.com/replypen/replypen-cli/internal/token"
)

// loadConfig is config resolution under the global --env flag; every command resolves through it.
func (e *env) loadConfig() (config.Resolved, error) {
	return config.LoadIn(e.envName, e.profile, e.project)
}

// noteEnv discloses a non-production server on stderr once per process, so "which box did that hit?"
// is never silent; no line means prod. stdout stays machine-clean.
func (e *env) noteEnv(res config.Resolved, base string) {
	if e.envNoted || e.baseURLOvr != "" || config.EnvOf(config.CanonicalBaseURL(base)) == config.EnvProd {
		return
	}
	e.envNoted = true
	_, _ = fmt.Fprintf(e.err, "env: %s · %s (%s)\n", config.EnvOf(config.CanonicalBaseURL(base)), base, res.BaseURLSource)
}

// loadStored loads profile's token for the server at base (config.StoreKey). A pre-namespace bare entry
// is never adopted for another server: logging in there again is cheaper than a race-free move.
func loadStored(profile, base string) (token.Token, bool, error) {
	return token.Load(config.StoreKey(base, profile))
}

// sameServer reports whether a token's recorded issuer is base. An empty issuer predates recording
// and is accepted only for production, where every such token was minted.
func sameServer(issuer, base string) bool {
	base = config.CanonicalBaseURL(base)
	if issuer == "" {
		return base == config.DefaultBaseURL
	}
	return config.CanonicalBaseURL(issuer) == base
}

// loginCommand is the copy-pasteable sign-in for base's server.
func loginCommand(base string) string {
	switch env := config.EnvOf(config.CanonicalBaseURL(base)); env {
	case config.EnvProd:
		return "rp auth login"
	case config.EnvCustom:
		return "REPLYPEN_BASE_URL=" + base + " rp auth login"
	default:
		return "rp --env " + env + " auth login"
	}
}

// onServer is a " on <env>" suffix for messages about a non-production server ("" for prod).
func onServer(base string) string {
	switch env := config.EnvOf(config.CanonicalBaseURL(base)); env {
	case config.EnvProd:
		return ""
	case config.EnvCustom:
		return " on " + base
	default:
		return " on " + env
	}
}

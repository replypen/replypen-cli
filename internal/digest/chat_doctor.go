package digest

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/replypen/replypen-cli/internal/client"
)

// DNSResult is the outcome of looking up one configured chat origin's host. The CLI performs the lookup;
// the diagnosis only ever flags an authoritative NXDOMAIN — a timeout or resolver error says nothing
// about the origin and stays silent.
type DNSResult string

const (
	DNSResolved DNSResult = "resolved"
	DNSNotFound DNSResult = "not_found" // authoritative NXDOMAIN
	DNSError    DNSResult = "error"     // timeout or resolver failure
)

// ChatDoctorInputs is everything `rp project chat doctor` fetched or probed, already decoded. The CLI
// owns the I/O (HTTP, DNS, the loader probe); ChatDoctor turns this into the verdict.
type ChatDoctorInputs struct {
	Settings   client.Settings          // GET /chat
	Principals client.PrincipalManifest // GET /principals
	// SecretSource is GET /chat/secret's "source": "dedicated" or the webhook fallback.
	SecretSource string
	Branding     client.Settings // GET /branding; zero when BrandingError is set
	// BrandingError is non-empty when the branding fetch failed — a finding, never fatal.
	BrandingError string
	Rejects       []client.ChatReject // GET /chat/rejects
	// OriginDNS maps each configured origin the CLI looked up to its outcome. Origins it skipped
	// (localhost, IP literals) are absent.
	OriginDNS map[string]DNSResult
	// WidgetLoaderStatus is the HTTP status of the loader probe; WidgetLoaderNetworkError marks a probe
	// that never got a response.
	WidgetLoaderStatus       int
	WidgetLoaderNetworkError bool
	Origin                   string        // --origin, "" when not given
	PrincipalKind            string        // --principal-kind, "" when not given
	Since                    time.Duration // --since: rejects older than now-Since are stale
}

// ChatDoctorFinding is one check result: status "ok" | "warning" | "failed".
type ChatDoctorFinding struct {
	Status string `json:"status"`
	Check  string `json:"check"`
	Code   string `json:"code"`
	Hint   string `json:"hint"`
	Docs   string `json:"docs"`
}

// ChatDoctorReject is one reject row as the report shows it: the server row plus the staleness verdict.
type ChatDoctorReject struct {
	Code      string    `json:"code"`
	Kind      string    `json:"kind,omitempty"`
	Origin    string    `json:"origin,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Stale     bool      `json:"stale,omitempty"`
}

// ChatDoctorPrincipals summarises the manifest without its (private) SQL.
type ChatDoctorPrincipals struct {
	Configured            bool     `json:"configured"`
	EmailLookupConfigured bool     `json:"email_lookup_configured"`
	Kinds                 []string `json:"kinds"`
}

// ChatDoctorBranding summarises the branding bag.
type ChatDoctorBranding struct {
	NameConfigured         bool `json:"name_configured"`
	PrimaryColorConfigured bool `json:"primary_color_configured"`
	Reachable              bool `json:"reachable"`
}

// ChatDoctorProbes is the widget loader probe outcome.
type ChatDoctorProbes struct {
	WidgetLoaderError  string `json:"widget_loader_error,omitempty"`
	WidgetLoaderStatus int    `json:"widget_loader_status"`
}

// ChatDoctorReport is the diagnosis. Failed is decided ONCE here, so the exit status cannot depend on the
// output mode (a piped doctor must exit non-zero on broken wiring too).
type ChatDoctorReport struct {
	Principals ChatDoctorPrincipals
	Branding   ChatDoctorBranding
	Probes     ChatDoctorProbes
	Rejects    []ChatDoctorReject
	Findings   []ChatDoctorFinding
	Failed     bool
}

// ChatDoctor diagnoses the embedded-chat wiring. Findings come in a fixed check order; the per-origin
// and per-reject warnings inside it are sorted, so two runs over the same inputs print the same report.
func ChatDoctor(in ChatDoctorInputs, now time.Time) ChatDoctorReport {
	kinds := in.Principals.KindNames()
	r := ChatDoctorReport{
		Principals: ChatDoctorPrincipals{Configured: len(kinds) > 0, EmailLookupConfigured: in.Principals.HasEmailLookup(), Kinds: kinds},
		Branding: ChatDoctorBranding{
			Reachable:              in.BrandingError == "",
			NameConfigured:         SettingString(in.Branding, "name") != "",
			PrimaryColorConfigured: SettingString(in.Branding, "primary_color") != "",
		},
		Probes: ChatDoctorProbes{WidgetLoaderStatus: in.WidgetLoaderStatus},
	}
	if in.WidgetLoaderNetworkError {
		r.Probes.WidgetLoaderError = "network error"
	}
	if in.Rejects != nil {
		cutoff := now.Add(-in.Since)
		r.Rejects = make([]ChatDoctorReject, len(in.Rejects))
		for i, x := range in.Rejects {
			r.Rejects[i] = ChatDoctorReject{Code: x.Code, Kind: x.Kind, Origin: x.Origin, Timestamp: x.Timestamp, Stale: x.Timestamp.Before(cutoff)}
		}
	}

	add := func(ok bool, good, bad, hint string) {
		status, code, check, docs := "ok", "OK", good, ""
		if ok {
			hint = "" // a passing check has no anchor in the integrator error index and no fix to suggest
		} else {
			status, code, check, docs = "failed", bad, bad, EmbassyDocsFor(bad)
		}
		r.Findings = append(r.Findings, ChatDoctorFinding{Status: status, Check: check, Code: code, Hint: hint, Docs: docs})
	}
	warn := func(code, hint string) {
		r.Findings = append(r.Findings, ChatDoctorFinding{Status: "warning", Check: code, Code: code, Hint: hint, Docs: EmbassyDocsFor(code)})
	}

	add(SettingBool(in.Settings, "chat_enabled"), "CHAT_ENABLED", "CHAT_DISABLED", "Enable chat for this project.")
	origins := SettingStrings(in.Settings, "chat_origins")
	if in.Origin != "" {
		add(slices.Contains(origins, in.Origin), "ORIGIN_ALLOWED", "ORIGIN_NOT_ALLOWED", "Add this exact origin to chat_origins.")
	} else {
		add(len(origins) > 0, "ORIGINS_CONFIGURED", "ORIGIN_NOT_ALLOWED", "Configure at least one exact chat origin.")
	}
	// A near-miss typo (staging.app.x vs app-staging.x) usually does not resolve, and nothing else flags
	// it: the page on the real host just fails ORIGIN_NOT_ALLOWED.
	for _, o := range origins {
		if in.OriginDNS[o] == DNSNotFound {
			warn("ORIGIN_UNRESOLVABLE", fmt.Sprintf("chat_origins entry %s has no DNS record — likely a typo of the real app host.", o))
		}
	}
	if in.Origin != "" && !slices.Contains(origins, in.Origin) {
		if near, ok := nearestOrigin(in.Origin, origins); ok {
			warn("ORIGIN_NEAR_MISS", fmt.Sprintf("%s is not registered, but %s is — one of them is probably a typo.", in.Origin, near))
		}
	}
	nearMisses := nearMissRejects(r.Rejects, origins)
	for _, rejected := range slices.Sorted(maps.Keys(nearMisses)) {
		if rejected == in.Origin {
			continue
		}
		warn("ORIGIN_NEAR_MISS", fmt.Sprintf("recent rejects came from %s, which is not registered; %s is — one of them is probably a typo.", rejected, nearMisses[rejected]))
	}
	if in.SecretSource == "dedicated" {
		add(true, "CHAT_SECRET_PRESENT", "", "Dedicated chat signing secret is configured.")
	} else {
		warn("CHAT_SECRET_FALLBACK", "Rotate a dedicated chat secret; webhook fallback is temporary.")
	}
	switch {
	case in.PrincipalKind != "":
		add(slices.Contains(kinds, in.PrincipalKind), "PRINCIPAL_KIND_DECLARED", "UNKNOWN_PRINCIPAL_KIND", "Add the requested kind to the principal manifest.")
	case len(kinds) > 0:
		add(true, "PRINCIPALS_CONFIGURED", "", "Principal manifest is configured.")
	default:
		warn("PRINCIPALS_DORMANT", "Declare principal kinds when chat must be user-scoped.")
	}
	// A run-time principal failure is invisible in CONFIG: the manifest validates, the token mints, and
	// only the recent rejects show that the asserted identity never verified against the project's data.
	if code, principalKind, n := recentPrincipalRejects(r.Rejects); n > 0 {
		scope := ""
		if principalKind != "" {
			scope = " for principal kind " + principalKind
		}
		warn(code, fmt.Sprintf("last %d turn(s)%s failed principal verification — check the asserted external ID against the manifest's verify query.", n, scope))
	}
	add(in.BrandingError == "", "BRANDING_REACHABLE", "BRANDING_UNAVAILABLE", "Check the branding API and project scope.")
	add(!in.WidgetLoaderNetworkError && in.WidgetLoaderStatus == 200, "WIDGET_LOADER_REACHABLE", "WIDGET_SCRIPT_BLOCKED", "Allow the ReplyPen loader URL through network and CSP policy.")

	for _, f := range r.Findings {
		if f.Status == "failed" {
			r.Failed = true
			break
		}
	}
	return r
}

// recentPrincipalRejects returns the DOMINANT (code, principal kind) pair in the recent rejects window
// and its own count — not the total across codes — so the doctor names the ONE failure an integrator can
// act on, with the kind whose verify query to check. Ties break on code then kind for determinism (map
// iteration order is random).
func recentPrincipalRejects(rejects []ChatDoctorReject) (string, string, int) {
	type failure struct{ code, kind string }
	counts := map[failure]int{}
	for _, r := range rejects {
		if r.Stale {
			continue
		}
		switch r.Code {
		case "PRINCIPAL_UNVERIFIED", "PRINCIPAL_LOOKUP_FAILED":
			counts[failure{code: r.Code, kind: r.Kind}]++
		}
	}
	best, total := failure{}, 0
	for f, n := range counts {
		if total == 0 || n > total || (n == total && (f.code < best.code || (f.code == best.code && f.kind < best.kind))) {
			best, total = f, n
		}
	}
	return best.code, best.kind, total
}

// nearMissRejects pairs each recent origin rejection with the configured origin it most resembles, so a
// doctor run names the likely typo instead of just "not allowed".
func nearMissRejects(rejects []ChatDoctorReject, origins []string) map[string]string {
	out := map[string]string{}
	for _, r := range rejects {
		if r.Stale || r.Origin == "" || slices.Contains(origins, r.Origin) {
			continue
		}
		if r.Code != "ORIGIN_NOT_ALLOWED" && r.Code != "ORIGIN_MISMATCH" {
			continue
		}
		if near, ok := nearestOrigin(r.Origin, origins); ok {
			out[r.Origin] = near
		}
	}
	return out
}

// nearestOrigin returns the configured origin closest to target when it is a plausible typo: same
// registrable suffix (last two labels) or a host edit distance of at most 4.
func nearestOrigin(target string, origins []string) (string, bool) {
	th := OriginHost(target)
	if th == "" {
		return "", false
	}
	best, bestDist := "", 1<<30
	for _, o := range origins {
		oh := OriginHost(o)
		if oh == "" || oh == th {
			continue
		}
		d := levenshtein(th, oh)
		if d <= 4 || registrable(th) == registrable(oh) {
			if d < bestDist {
				best, bestDist = o, d
			}
		}
	}
	return best, best != ""
}

// OriginHost is the lower-cased host of an origin URL, "" when it does not parse.
func OriginHost(origin string) string {
	u, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func registrable(host string) string {
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return host
	}
	return strings.Join(labels[len(labels)-2:], ".")
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

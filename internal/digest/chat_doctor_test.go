package digest

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rootcause-org/rootcause-cli/internal/client"
)

var doctorNow = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

// healthyDoctorInputs is a fully wired project: every check passes, nothing warns.
func healthyDoctorInputs() ChatDoctorInputs {
	return ChatDoctorInputs{
		Settings: client.Settings{
			"chat_enabled": {Effective: json.RawMessage(`true`)},
			"chat_origins": {Effective: json.RawMessage(`["https://app.example.com"]`)},
		},
		Principals:         client.PrincipalManifest{Kinds: map[string]json.RawMessage{"app_user": json.RawMessage(`{}`)}},
		SecretSource:       "dedicated",
		Branding:           client.Settings{"name": {Effective: json.RawMessage(`"Example"`)}},
		OriginDNS:          map[string]DNSResult{"https://app.example.com": DNSResolved},
		WidgetLoaderStatus: 200,
		Since:              time.Hour,
	}
}

func findingCodes(r ChatDoctorReport, status string) []string {
	var out []string
	for _, f := range r.Findings {
		if f.Status == status {
			out = append(out, f.Code)
		}
	}
	return out
}

func TestChatDoctorStaleBoundary(t *testing.T) {
	in := healthyDoctorInputs()
	in.Rejects = []client.ChatReject{
		{Code: "BAD_TOKEN", Timestamp: doctorNow.Add(-time.Hour)},                // exactly at the cutoff: still recent
		{Code: "BAD_TOKEN", Timestamp: doctorNow.Add(-time.Hour - time.Second)},  // just past it: stale
		{Code: "PRINCIPAL_UNVERIFIED", Timestamp: doctorNow.Add(-2 * time.Hour)}, // stale: no principal warning
	}
	r := ChatDoctor(in, doctorNow)
	if got := []bool{r.Rejects[0].Stale, r.Rejects[1].Stale, r.Rejects[2].Stale}; !reflect.DeepEqual(got, []bool{false, true, true}) {
		t.Fatalf("stale = %v, want [false true true]", got)
	}
	if w := findingCodes(r, "warning"); len(w) != 0 {
		t.Fatalf("a stale principal reject still warned: %v", w)
	}
}

func TestChatDoctorDeterministicOrder(t *testing.T) {
	in := healthyDoctorInputs()
	in.Settings["chat_origins"] = client.SettingField{Effective: json.RawMessage(`["https://z.example.com","https://a.example.com"]`)}
	in.OriginDNS = map[string]DNSResult{"https://a.example.com": DNSNotFound, "https://z.example.com": DNSNotFound}
	for _, host := range []string{"f", "c", "e", "b", "d"} {
		in.Rejects = append(in.Rejects, client.ChatReject{Code: "ORIGIN_NOT_ALLOWED", Origin: "https://" + host + ".example.com", Timestamp: doctorNow})
	}
	first := ChatDoctor(in, doctorNow)
	var hints []string
	for _, f := range first.Findings {
		if f.Status == "warning" {
			hints = append(hints, strings.Fields(f.Hint)[4])
		}
	}
	// Unresolvable origins follow chat_origins order; near-miss rejects are sorted by rejected origin.
	want := []string{"no", "no", "https://b.example.com,", "https://c.example.com,", "https://d.example.com,", "https://e.example.com,", "https://f.example.com,"}
	if !reflect.DeepEqual(hints, want) {
		t.Fatalf("warning order = %q, want %q", hints, want)
	}
	if !strings.Contains(first.Findings[2].Hint, "z.example.com") || !strings.Contains(first.Findings[3].Hint, "a.example.com") {
		t.Fatalf("unresolvable warnings out of chat_origins order: %+v", first.Findings[2:4])
	}
	for i := 0; i < 20; i++ {
		if again := ChatDoctor(in, doctorNow); !reflect.DeepEqual(again, first) {
			t.Fatalf("run %d differs from the first", i)
		}
	}
}

func TestChatDoctorFlagsOnlyNXDOMAIN(t *testing.T) {
	in := healthyDoctorInputs()
	in.Settings["chat_origins"] = client.SettingField{Effective: json.RawMessage(`["https://typo.example.com","https://flaky.example.com","http://localhost:3000"]`)}
	in.OriginDNS = map[string]DNSResult{"https://typo.example.com": DNSNotFound, "https://flaky.example.com": DNSError}
	r := ChatDoctor(in, doctorNow)
	var unresolvable []string
	for _, f := range r.Findings {
		if f.Code == "ORIGIN_UNRESOLVABLE" {
			unresolvable = append(unresolvable, f.Hint)
		}
	}
	if len(unresolvable) != 1 || !strings.Contains(unresolvable[0], "typo.example.com") {
		t.Fatalf("ORIGIN_UNRESOLVABLE = %q, want only the NXDOMAIN origin", unresolvable)
	}
}

func TestChatDoctorBrandingFailureIsAFindingNotFatal(t *testing.T) {
	in := healthyDoctorInputs()
	in.Branding, in.BrandingError = nil, "HTTP 403"
	r := ChatDoctor(in, doctorNow)
	if !r.Failed || r.Branding.Reachable || r.Branding.NameConfigured {
		t.Fatalf("branding failure: failed=%t branding=%+v", r.Failed, r.Branding)
	}
	if got := findingCodes(r, "failed"); !reflect.DeepEqual(got, []string{"BRANDING_UNAVAILABLE"}) {
		t.Fatalf("failed = %v, want only BRANDING_UNAVAILABLE — the other checks must still run", got)
	}
	if last := r.Findings[len(r.Findings)-1]; last.Code != "OK" || last.Check != "WIDGET_LOADER_REACHABLE" {
		t.Fatalf("checks after branding did not run: %+v", last)
	}
}

func TestChatDoctorWarningsAloneSucceed(t *testing.T) {
	in := healthyDoctorInputs()
	in.SecretSource = "webhook"
	in.Principals = client.PrincipalManifest{}
	r := ChatDoctor(in, doctorNow)
	if r.Failed {
		t.Fatalf("warnings only must not fail: %+v", r.Findings)
	}
	if got := findingCodes(r, "warning"); !reflect.DeepEqual(got, []string{"CHAT_SECRET_FALLBACK", "PRINCIPALS_DORMANT"}) {
		t.Fatalf("warnings = %v", got)
	}
	if healthy := ChatDoctor(healthyDoctorInputs(), doctorNow); healthy.Failed || len(findingCodes(healthy, "warning")) != 0 {
		t.Fatalf("healthy inputs: %+v", healthy.Findings)
	}
}

func TestChatDoctorWidgetProbeOutcomes(t *testing.T) {
	for name, tc := range map[string]struct {
		status  int
		netErr  bool
		failed  bool
		errText string
	}{
		"ok":            {status: 200},
		"blocked":       {status: 404, failed: true},
		"network error": {netErr: true, failed: true, errText: "network error"},
	} {
		in := healthyDoctorInputs()
		in.WidgetLoaderStatus, in.WidgetLoaderNetworkError = tc.status, tc.netErr
		r := ChatDoctor(in, doctorNow)
		if r.Failed != tc.failed || r.Probes.WidgetLoaderError != tc.errText || r.Probes.WidgetLoaderStatus != tc.status {
			t.Fatalf("%s: failed=%t probes=%+v", name, r.Failed, r.Probes)
		}
	}
}

// The doctor names the ONE run-time principal failure an integrator can act on: the dominant
// (code, kind) pair with its own count.
func TestRecentPrincipalRejects(t *testing.T) {
	code, kind, n := recentPrincipalRejects([]ChatDoctorReject{
		{Code: "ORIGIN_NOT_ALLOWED"},
		{Code: "PRINCIPAL_LOOKUP_FAILED", Kind: "account"},
		{Code: "PRINCIPAL_UNVERIFIED", Kind: "app_user"},
		{Code: "PRINCIPAL_UNVERIFIED", Kind: "ignored", Stale: true},
		{Code: "PRINCIPAL_UNVERIFIED", Kind: "app_user"},
	})
	if code != "PRINCIPAL_UNVERIFIED" || kind != "app_user" || n != 2 {
		t.Fatalf("recentPrincipalRejects = %q/%q/%d, want PRINCIPAL_UNVERIFIED/app_user/2", code, kind, n)
	}
	if code, kind, n := recentPrincipalRejects([]ChatDoctorReject{{Code: "BAD_TOKEN"}}); code != "" || kind != "" || n != 0 {
		t.Fatalf("recentPrincipalRejects = %q/%q/%d, want no finding", code, kind, n)
	}
}

func TestNearMissRejectsNamesTheLikelyTypo(t *testing.T) {
	origins := []string{"https://staging.app.example.com", "https://app.other.io"}
	got := nearMissRejects([]ChatDoctorReject{
		{Code: "ORIGIN_NOT_ALLOWED", Origin: "https://app-staging.example.com"},
		{Code: "ORIGIN_NOT_ALLOWED", Origin: "https://unrelated.dev"},
		{Code: "ORIGIN_NOT_ALLOWED", Origin: "https://app.example.com", Stale: true},
		{Code: "TOKEN_REPLAYED", Origin: "https://app.example.com"},
	}, origins)
	if len(got) != 1 || got["https://app-staging.example.com"] != "https://staging.app.example.com" {
		t.Fatalf("nearMissRejects = %v", got)
	}
}

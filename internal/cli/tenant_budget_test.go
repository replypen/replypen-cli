package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestTenantBudgetCommands pins the wire shape (path, method, exactly-one body key incl. a negative add)
// and the plain-text view that says the limit is recurring and shared.
func TestTenantBudgetCommands(t *testing.T) {
	const resp = `{"metered":true,"account":"kampadmin:heyo","monthly_usd":125.5,"used_percent":37,"paused":false,"resets_at":"2026-10-31T23:00:00Z","tenants":["kampadmin-support/heyo","kampadmin/heyo"]}`
	var method, path, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireAuth(t, r)
		if r.URL.Path == "/api/v1/whoami" {
			_, _ = w.Write([]byte(`{"project":{"name":"kampadmin-support"}}`))
			return
		}
		method, path, body = r.Method, r.URL.Path, readBody(t, r)
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/projects/kampadmin-support/ai-budgets" {
			_, _ = w.Write([]byte(`{"tenants":[` + strings.Replace(resp, "{", `{"tenant":"heyo",`, 1) + `,{"tenant":"impact","metered":false,"monthly_usd":null,"used_percent":null,"paused":false,"resets_at":"2026-10-31T23:00:00Z","tenants":[]}]}`))
			return
		}
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()

	for _, tc := range []struct {
		args       []string
		method     string
		body       string
		wantOutput string
	}{
		{[]string{"get", "heyo"}, http.MethodGet, "", "limit      $125.50 per month (recurring)"},
		{[]string{"set", "heyo", "--usd", "125.50"}, http.MethodPatch, `{"monthly_usd":125.5}`, "shared by  kampadmin-support/heyo, kampadmin/heyo"},
		{[]string{"add", "heyo", "--usd", "-10"}, http.MethodPatch, `{"add_usd":-10}`, "used       37% this month, resets 1 Nov 2026"},
	} {
		e, out, _ := newTestEnv(t, srv, "table")
		args := append([]string{"project", "tenant", "budget"}, tc.args...)
		if err := run(t, e, args...); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if method != tc.method || path != "/api/v1/projects/kampadmin-support/tenants/heyo/ai-budget/limit" || strings.TrimSpace(body) != tc.body {
			t.Fatalf("%v sent %s %s %q", tc.args, method, path, body)
		}
		if !strings.Contains(out.String(), tc.wantOutput) {
			t.Fatalf("%v output:\n%s\nwant %q", tc.args, out.String(), tc.wantOutput)
		}
	}

	e, out, _ := newTestEnv(t, srv, "table")
	if err := run(t, e, "project", "tenant", "budget", "ls"); err != nil {
		t.Fatal(err)
	}
	if s := out.String(); !strings.Contains(s, "heyo") || !strings.Contains(s, "$125.50") || !strings.Contains(s, "impact") || !strings.Contains(s, "none") {
		t.Fatalf("ls output:\n%s", s)
	}

	e, _, _ = newTestEnv(t, srv, "table")
	if err := run(t, e, "project", "tenant", "budget", "add", "heyo"); err == nil || !strings.Contains(err.Error(), "--usd") {
		t.Fatalf("missing --usd err = %v", err)
	}
}

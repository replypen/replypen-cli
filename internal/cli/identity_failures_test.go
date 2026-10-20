package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIdentityFailuresFiltersAndCursor(t *testing.T) {
	base := stubServer(t)
	defer base.Close()
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/admin/projects/alpha/identity-failures" {
			requests++
			if r.URL.Query().Get("simulation") != "true" || r.URL.Query().Get("cursor") != "abc+/=" {
				t.Errorf("filters = %s", r.URL.RawQuery)
			}
		}
		base.Config.Handler.ServeHTTP(w, r)
	}))
	defer srv.Close()
	e, _, stderr := newTestEnv(t, srv, "table")
	if err := run(t, e, "admin", "project", "identity-failures", "alpha", "--simulation", "--cursor", "abc+/="); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || !strings.Contains(stderr.String(), "more: --cursor next-page") {
		t.Fatalf("requests=%d stderr=%s", requests, stderr)
	}
}

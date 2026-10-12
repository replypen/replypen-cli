package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestTokenLs pins the last-used review: table default (active only, never-used first), --wide, --all,
// and that -o json keeps the envelope + raw keys while applying the same filter and sort.
func TestTokenLs(t *testing.T) {
	pinNow(t, "2026-10-12T12:00:00Z")
	for _, tc := range []struct {
		name, golden string
		args         []string
	}{
		{"Default", "token_ls.golden", []string{"project", "token", "ls"}},
		{"Wide", "token_ls_wide.golden", []string{"project", "token", "ls", "--wide"}},
		{"RevokedUnused30d", "token_ls_unused.golden", []string{"project", "token", "ls", "--include-revoked", "--unused-days", "30"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := stubServer(t)
			defer srv.Close()
			e, out, _ := newTestEnv(t, srv, "table")
			if err := run(t, e, tc.args...); err != nil {
				t.Fatal(err)
			}
			assertGolden(t, tc.golden, out.String())
		})
	}

	t.Run("JSONFilterKeepsEnvelope", func(t *testing.T) {
		srv := stubServer(t)
		defer srv.Close()
		e, out, _ := newTestEnv(t, srv, "json")
		if err := run(t, e, "project", "token", "ls", "--unused-days", "30"); err != nil {
			t.Fatal(err)
		}
		var got struct {
			Tokens []map[string]any `json:"tokens"`
		}
		if err := json.Unmarshal(out.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, it := range got.Tokens {
			ids = append(ids, it["id"].(string))
		}
		if want := "tok_aaaa,tok_eeee,tok_dddd"; strings.Join(ids, ",") != want {
			t.Fatalf("ids = %v, want %s", ids, want)
		}
		if got.Tokens[2]["last_used_user_agent"] != "curl/8.4.0" {
			t.Errorf("raw keys dropped: %v", got.Tokens[2])
		}
	})

	t.Run("RejectsNegativeUnusedDays", func(t *testing.T) {
		srv := stubServer(t)
		defer srv.Close()
		e, _, _ := newTestEnv(t, srv, "table")
		if err := run(t, e, "project", "token", "ls", "--unused-days", "-1"); err == nil {
			t.Fatal("want error")
		}
	})
}

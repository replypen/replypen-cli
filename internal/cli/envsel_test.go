package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/replypen/replypen-cli/internal/config"
	"github.com/replypen/replypen-cli/internal/token"
)

// A login on another server lands under that server's key and leaves the prod entry untouched (before
// namespacing, a staging login overwrote prod "default").
func TestLoginOnOtherServerKeepsProdEntry(t *testing.T) {
	isolatedConfig(t)
	prod := token.Token{AccessToken: "rcoa_prod", RefreshToken: "rcor_prod", BaseURL: config.DefaultBaseURL, ExpiresAt: time.Now().Add(time.Hour)}
	seedToken(t, "default", prod)
	stub := newOAuthStub(t)
	devicePollWait = func(time.Duration) <-chan time.Time { return time.After(time.Millisecond) }
	t.Cleanup(func() { devicePollWait = nil })
	t.Setenv("REPLYPEN_BASE_URL", stub.srv.URL)

	var out, errb bytes.Buffer
	if err := run(t, &env{profile: "default", out: &out, err: &errb}, "auth", "login", "--device"); err != nil {
		t.Fatalf("login: %v (stderr=%s)", err, errb.String())
	}
	if got, ok, _ := token.Load("default"); !ok || got.AccessToken != "rcoa_prod" {
		t.Fatalf("prod entry changed: %+v ok=%v", got, ok)
	}
	if got, ok, _ := token.Load(config.StoreKey(stub.srv.URL, "default")); !ok || got.AccessToken != "rcoa_first" {
		t.Fatalf("other-server login not stored under its key: %+v ok=%v", got, ok)
	}
}

// A stored token minted by another server is never presented: no request, a re-login naming the env.
func TestLiveSourceRefusesTokenFromOtherServer(t *testing.T) {
	isolatedConfig(t)
	stub := newOAuthStub(t)
	seedToken(t, "default", token.Token{AccessToken: "rcoa_x", RefreshToken: "rcor_x", BaseURL: stub.srv.URL, ExpiresAt: time.Now().Add(-time.Hour)})
	// The same entry, as a pre-namespace store would have it under prod's bare key.
	stored, _, _ := token.Load(config.StoreKey(stub.srv.URL, "default"))
	if err := token.Save("default", stored); err != nil {
		t.Fatal(err)
	}

	_, err := newLiveSource("default", config.DefaultBaseURL).Token(t.Context())
	if err == nil || !strings.Contains(err.Error(), "holds a token from "+stub.srv.URL) || !strings.Contains(err.Error(), "rp auth login") {
		t.Fatalf("err = %v, want a cross-server refusal", err)
	}
	if stub.refreshes.Load() != 0 {
		t.Fatalf("refresh sent to the wrong server: %d", stub.refreshes.Load())
	}
}

// A pre-namespace bare entry answers only for prod, never for the server it may have been minted on.
func TestBareLegacyEntryOnlyAnswersProd(t *testing.T) {
	isolatedConfig(t)
	local := "http://localhost:24729"
	if err := token.Save("local", token.Token{AccessToken: "rcoa_local", BaseURL: local}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := loadStored("local", local); ok {
		t.Fatal("a bare entry must not answer for a namespaced server")
	}
	if _, ok, _ := loadStored("local", config.StagingBaseURL); ok {
		t.Fatal("a bare entry must not answer for staging")
	}
}

// Auth commands disclose a non-prod server too ("no env line means prod" holds for every command).
func TestAuthStatusNotesEnv(t *testing.T) {
	isolatedConfig(t)
	t.Setenv("RP_ENV", "staging")
	var out, errb bytes.Buffer
	t.Chdir(t.TempDir())
	if err := run(t, &env{out: &out, err: &errb, output: "json"}, "auth", "status"); err != nil {
		t.Fatalf("auth status: %v", err)
	}
	if !strings.HasPrefix(errb.String(), "env: staging · "+config.StagingBaseURL) {
		t.Fatalf("stderr = %q, want the env line", errb.String())
	}
}

func TestLoginCommandNamesEnv(t *testing.T) {
	for base, want := range map[string]string{
		config.DefaultBaseURL:    "rp auth login",
		config.StagingBaseURL:    "rp --env staging auth login",
		"http://localhost:24729": "REPLYPEN_BASE_URL=http://localhost:24729 rp auth login",
	} {
		if got := loginCommand(base); got != want {
			t.Errorf("loginCommand(%q) = %q, want %q", base, got, want)
		}
	}
}

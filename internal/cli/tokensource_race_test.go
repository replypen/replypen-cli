package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rootcause-org/rootcause-cli/internal/token"
)

// TestConcurrentProcessesRefreshOnce: parallel `rc` processes on one profile (e.g. devloop's batch of
// `rc ask --no-wait`) all see the same expired access token. The server rotates refresh tokens and
// accepts each exactly once, so only ONE refresh may be sent; every process must still end with the
// renewed access token instead of a spurious "session expired".
func TestConcurrentProcessesRefreshOnce(t *testing.T) {
	isolatedConfig(t)

	var (
		mu        sync.Mutex
		used      = map[string]bool{}
		refreshes int
	)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		mu.Lock()
		refreshes++
		n := refreshes
		rt := r.PostFormValue("refresh_token")
		replay := used[rt]
		used[rt] = true
		mu.Unlock()
		time.Sleep(300 * time.Millisecond) // widen the window: siblings start while this refresh is in flight
		w.Header().Set("Content-Type", "application/json")
		if replay {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
			return
		}
		_, _ = fmt.Fprintf(w, `{"access_token":"rcoa_gen%d","refresh_token":"rcor_gen%d","token_type":"Bearer","expires_in":3600}`, n, n)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	seedToken(t, "default", token.Token{
		AccessToken: "rcoa_stale", RefreshToken: "rcor_live",
		ExpiresAt: time.Now().Add(-time.Hour), BaseURL: srv.URL,
	})

	const procs = 3
	outs := make([]string, procs)
	var wg sync.WaitGroup
	for i := range procs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestRefreshHelperProcess$")
			cmd.Env = append(os.Environ(), "RC_TEST_REFRESH_URL="+srv.URL)
			out, err := cmd.CombinedOutput()
			if err != nil {
				outs[i] = fmt.Sprintf("exec: %v: %s", err, out)
				return
			}
			outs[i] = string(out)
		}()
	}
	wg.Wait()

	for i, out := range outs {
		if !strings.Contains(out, "TOKEN=rcoa_gen1\n") {
			t.Errorf("process %d did not end with the renewed token:\n%s", i, out)
		}
	}
	if refreshes != 1 {
		t.Errorf("refresh requests = %d, want exactly 1", refreshes)
	}
	if stored, _, _ := token.Load("default"); stored.AccessToken != "rcoa_gen1" || stored.RefreshToken != "rcor_gen1" {
		t.Errorf("stored token = %+v, want the single rotated pair", stored)
	}
}

// TestRefreshHelperProcess is the child half of TestConcurrentProcessesRefreshOnce: one `rc` process
// asking its token source for a bearer. It inherits XDG_CONFIG_HOME, so all children share one store.
func TestRefreshHelperProcess(t *testing.T) {
	url := os.Getenv("RC_TEST_REFRESH_URL")
	if url == "" {
		t.Skip("helper process for TestConcurrentProcessesRefreshOnce")
	}
	got, err := newLiveSource("default", url).Token(t.Context())
	if err != nil {
		fmt.Printf("ERR=%v\n", err)
		return
	}
	fmt.Printf("TOKEN=%s\n", got)
}

// TestForcedRefreshSkipsWhenSiblingRenewed: a 401 forces a refresh of the token we handed out, unless
// the store already holds a different, unexpired token (a sibling renewed it) — then we adopt it.
func TestForcedRefreshSkipsWhenSiblingRenewed(t *testing.T) {
	isolatedConfig(t)
	stub := newOAuthStub(t)
	seedToken(t, "default", token.Token{
		AccessToken: "rcoa_live", RefreshToken: "rcor_live",
		ExpiresAt: time.Now().Add(time.Hour), BaseURL: stub.srv.URL,
	})
	src := newLiveSource("default", stub.srv.URL)
	if got, _ := src.Token(t.Context()); got != "rcoa_live" {
		t.Fatalf("Token = %q", got)
	}
	// The server rejected rcoa_live → forced refresh.
	if got, err := src.Refresh(t.Context()); err != nil || got != "rcoa_refreshed" {
		t.Fatalf("Refresh = %q, %v; want rcoa_refreshed", got, err)
	}
	// A sibling renews before our next 401 → adopt its token, no request.
	seedToken(t, "default", token.Token{
		AccessToken: "rcoa_sibling", RefreshToken: "rcor_sibling",
		ExpiresAt: time.Now().Add(time.Hour), BaseURL: stub.srv.URL,
	})
	if got, err := src.Refresh(t.Context()); err != nil || got != "rcoa_sibling" {
		t.Fatalf("Refresh = %q, %v; want the sibling's token", got, err)
	}
	if n := stub.refreshes.Load(); n != 1 {
		t.Errorf("refresh requests = %d, want 1", n)
	}
}

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

	"github.com/replypen/replypen-cli/internal/token"
)

// TestConcurrentProcessesRefreshOnce: parallel `rp` processes on one profile (e.g. devloop's batch of
// `rp ask --no-wait`) all see the same expired access token. The server rotates refresh tokens and
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

// TestRefreshHelperProcess is the child half of the multi-process refresh tests: one `rp` process asking
// its token source (profile RC_TEST_REFRESH_PROFILE, default "default") for a bearer. It inherits
// XDG_CONFIG_HOME, so all children share one store.
func TestRefreshHelperProcess(t *testing.T) {
	url := os.Getenv("RC_TEST_REFRESH_URL")
	if url == "" {
		t.Skip("helper process for TestConcurrentProcessesRefreshOnce")
	}
	profile := os.Getenv("RC_TEST_REFRESH_PROFILE")
	if profile == "" {
		profile = "default"
	}
	got, err := newLiveSource(profile, url).Token(t.Context())
	if err != nil {
		fmt.Printf("ERR=%v\n", err)
		return
	}
	fmt.Printf("TOKEN=%s\n", got)
}

// TestConcurrentProfilesRefreshInParallel: refresh serialization is per profile. Three processes on three
// expired profiles must refresh concurrently — a slow OAuth round-trip on one profile may not queue the
// others behind it (a store-wide lock made them take N× the latency, and time out once N× exceeded the
// lock wait). Each refresh holds its response until all three are in flight: parallel refreshes meet at
// that barrier at once, serialized ones never do and the first one gives up after barrierWait.
func TestConcurrentProfilesRefreshInParallel(t *testing.T) {
	isolatedConfig(t)

	const barrierWait = 10 * time.Second
	profiles := []string{"alpha", "beta", "gamma"}
	var (
		mu        sync.Mutex
		refreshes = map[string]int{}
		inFlight  sync.WaitGroup
		allIn     = make(chan struct{})
		met       = true
	)
	inFlight.Add(len(profiles))
	go func() { inFlight.Wait(); close(allIn) }()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		rt := r.PostFormValue("refresh_token") // rcor_<profile>
		mu.Lock()
		refreshes[rt]++
		first := refreshes[rt] == 1
		mu.Unlock()
		if first {
			inFlight.Done()
		}
		select {
		case <-allIn:
		case <-time.After(barrierWait):
			mu.Lock()
			met = false
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"access_token":"rcoa_new_%s","refresh_token":"rcor_new_%s","token_type":"Bearer","expires_in":3600}`,
			strings.TrimPrefix(rt, "rcor_"), strings.TrimPrefix(rt, "rcor_"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, p := range profiles {
		seedToken(t, p, token.Token{
			AccessToken: "rcoa_stale", RefreshToken: "rcor_" + p,
			ExpiresAt: time.Now().Add(-time.Hour), BaseURL: srv.URL,
		})
	}

	outs := make([]string, len(profiles))
	var wg sync.WaitGroup
	for i, p := range profiles {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestRefreshHelperProcess$")
			cmd.Env = append(os.Environ(), "RC_TEST_REFRESH_URL="+srv.URL, "RC_TEST_REFRESH_PROFILE="+p)
			out, err := cmd.CombinedOutput()
			if err != nil {
				outs[i] = fmt.Sprintf("exec: %v: %s", err, out)
				return
			}
			outs[i] = string(out)
		}()
	}
	wg.Wait()

	for i, p := range profiles {
		if !strings.Contains(outs[i], "TOKEN=rcoa_new_"+p+"\n") {
			t.Errorf("profile %s did not end with its renewed token:\n%s", p, outs[i])
		}
		stored, _, _ := token.Load(p)
		if stored.AccessToken != "rcoa_new_"+p || stored.RefreshToken != "rcor_new_"+p {
			t.Errorf("stored %s = %+v, want its rotated pair", p, stored)
		}
		if n := refreshes["rcor_"+p]; n != 1 {
			t.Errorf("profile %s refresh requests = %d, want 1", p, n)
		}
	}
	if !met {
		t.Errorf("the three refreshes were never in flight together: profiles refreshed serially, not in parallel")
	}
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

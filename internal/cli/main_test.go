package cli

import (
	"os"
	"testing"
)

// TestMain points the token store at a throwaway config base: any test that reaches token.Load without
// its own XDG_CONFIG_HOME would otherwise read — and migrate — the developer's real ~/.config.
func TestMain(m *testing.M) {
	if os.Getenv("RC_TEST_REFRESH_URL") != "" { // a refresh helper child shares its parent's store
		os.Exit(m.Run())
	}
	dir, err := os.MkdirTemp("", "rp-cli-test-config-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("XDG_CONFIG_HOME", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

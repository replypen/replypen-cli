package cli

import (
	"context"
	"net"
	"testing"

	"github.com/rootcause-org/rootcause-cli/internal/digest"
)

func TestLookupOriginsClassifiesNXDOMAINApartFromTimeouts(t *testing.T) {
	lookup := func(_ context.Context, host string) ([]string, error) {
		switch host {
		case "staging.app.example.com":
			return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
		case "flaky.example.com":
			return nil, &net.DNSError{Err: "timeout", Name: host, IsTimeout: true}
		}
		return []string{"1.2.3.4"}, nil
	}
	got := lookupOrigins(context.Background(), []string{
		"https://app-staging.example.com", "https://staging.app.example.com",
		"https://flaky.example.com", "http://localhost:3333", "http://127.0.0.1:8080",
	}, lookup)
	want := map[string]digest.DNSResult{
		"https://app-staging.example.com": digest.DNSResolved,
		"https://staging.app.example.com": digest.DNSNotFound,
		"https://flaky.example.com":       digest.DNSError,
	}
	if len(got) != len(want) {
		t.Fatalf("lookupOrigins = %v, want %v", got, want)
	}
	for o, r := range want {
		if got[o] != r {
			t.Fatalf("lookupOrigins[%s] = %q, want %q (all: %v)", o, got[o], r, got)
		}
	}
}

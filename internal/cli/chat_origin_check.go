package cli

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/rootcause-org/rootcause-cli/internal/digest"
)

// lookupOrigins resolves each configured chat origin's host, so the doctor can flag one with no DNS
// record. Only an authoritative NXDOMAIN maps to DNSNotFound; a timeout or resolver error is DNSError,
// which the diagnosis treats as "nothing learned". localhost and IP literals are not looked up.
func lookupOrigins(ctx context.Context, origins []string, lookup func(context.Context, string) ([]string, error)) map[string]digest.DNSResult {
	out := map[string]digest.DNSResult{}
	for _, o := range origins {
		host := digest.OriginHost(o)
		if host == "" || host == "localhost" || net.ParseIP(host) != nil {
			continue
		}
		lctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		_, err := lookup(lctx, host)
		cancel()
		var dnsErr *net.DNSError
		switch {
		case err == nil:
			out[o] = digest.DNSResolved
		case errors.As(err, &dnsErr) && dnsErr.IsNotFound:
			out[o] = digest.DNSNotFound
		default:
			out[o] = digest.DNSError
		}
	}
	return out
}

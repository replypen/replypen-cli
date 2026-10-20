package render

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/replypen/replypen-cli/internal/client"
)

func IdentityFailures(w io.Writer, list *client.IdentityFailureList) {
	if len(list.Items) == 0 {
		_, _ = fmt.Fprintln(w, "(no identity failures)")
		return
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "DATE\tTENANT\tCATEGORY\tRUN")
	for _, item := range list.Items {
		tenant := "Project-wide"
		if item.Tenant != nil {
			tenant = *item.Tenant
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", item.CreatedAt, tenant, identityCategory(item.Category), item.RunID)
	}
	_ = tw.Flush()
}

func identityCategory(kind string) string {
	switch kind {
	case "identity_unresolved":
		return "Identity unresolved"
	case "identity_ambiguous":
		return "Multiple possible identities"
	case "identity_lookup_unavailable":
		return "Identity lookup unavailable"
	default:
		return kind
	}
}

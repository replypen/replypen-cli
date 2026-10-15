package render

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// PathsReport is `rc dev console paths`: which absolute paths exist in a real production workspace, per
// checked tenant. It is a CLI-composed view over several `console bash run` calls (no server endpoint of
// its own), so the struct doubles as the -o json contract — field names are that contract.
type PathsReport struct {
	Project string        `json:"project"`
	Paths   []string      `json:"paths"`
	Results []PathsResult `json:"results"`
}

// PathsResult is one checked scope. Tenant is empty for the normal (no --tenants) scope on a project
// without tenants. Error set ⇒ Entries omitted: a failed check never reads as "missing".
type PathsResult struct {
	Tenant        string      `json:"tenant"`
	BrainResolved string      `json:"brain_resolved"`
	RunIDs        []string    `json:"run_ids"`
	Error         string      `json:"error"`
	Entries       []PathEntry `json:"entries,omitempty"`
}

// PathEntry kinds: "dir", "file" or "missing" (a dangling symlink is missing).
type PathEntry struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

const PathMissing = "missing"

// pathsWideTenants is the switch from one-column-per-tenant to the PRESENT/MISSING ON roll-up.
const pathsWideTenants = 3

// Paths renders the existence matrix: ≤3 checked scopes get a KIND column each, more get a roll-up of
// how many scopes have the path and which lack it. Errored scopes are listed below and never counted.
func Paths(w io.Writer, r PathsReport) {
	_, _ = fmt.Fprintf(w, "Project: %s\n", orDash(r.Project, "-"))
	wide := len(r.Results) <= pathsWideTenants
	if wide {
		for _, res := range r.Results {
			_, _ = fmt.Fprintf(w, "  %s: %s\n", pathsTenantLabel(res.Tenant), orDash(res.BrainResolved, "-"))
		}
	} else {
		// Many tenants usually share one brain commit: count per distinct value instead of 60 lines.
		var order []string
		counts := map[string]int{}
		for _, res := range r.Results {
			if res.Error != "" {
				continue
			}
			if counts[res.BrainResolved] == 0 {
				order = append(order, res.BrainResolved)
			}
			counts[res.BrainResolved]++
		}
		for _, brain := range order {
			_, _ = fmt.Fprintf(w, "  brain %s: %d tenants\n", orDash(brain, "-"), counts[brain])
		}
	}
	_, _ = fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if wide {
		header := []string{"PATH"}
		for _, res := range r.Results {
			header = append(header, strings.ToUpper(pathsTenantLabel(res.Tenant)))
		}
		_, _ = fmt.Fprintln(tw, strings.Join(header, "\t"))
		for i, p := range r.Paths {
			row := []string{p}
			for _, res := range r.Results {
				row = append(row, pathsCell(res, i))
			}
			_, _ = fmt.Fprintln(tw, strings.Join(row, "\t"))
		}
	} else {
		_, _ = fmt.Fprintln(tw, "PATH\tPRESENT\tMISSING ON")
		for i, p := range r.Paths {
			checked, present := 0, 0
			var missing []string
			for _, res := range r.Results {
				if res.Error != "" || i >= len(res.Entries) {
					continue
				}
				checked++
				if res.Entries[i].Kind == PathMissing {
					missing = append(missing, pathsTenantLabel(res.Tenant))
				} else {
					present++
				}
			}
			_, _ = fmt.Fprintf(tw, "%s\t%d/%d\t%s\n", p, present, checked, pathsMissingList(missing, 5))
		}
	}
	_ = tw.Flush()
	for _, res := range r.Results {
		if res.Error != "" {
			_, _ = fmt.Fprintf(w, "\nerror on %s: %s\n", pathsTenantLabel(res.Tenant), res.Error)
		}
	}
}

func pathsTenantLabel(tenant string) string {
	if tenant == "" {
		return "(project)"
	}
	return tenant
}

func pathsCell(res PathsResult, i int) string {
	if res.Error != "" || i >= len(res.Entries) {
		return "error"
	}
	return res.Entries[i].Kind
}

func pathsMissingList(missing []string, max int) string {
	if len(missing) == 0 {
		return "-"
	}
	if len(missing) <= max {
		return strings.Join(missing, ", ")
	}
	return fmt.Sprintf("%s +%d more", strings.Join(missing[:max], ", "), len(missing)-max)
}

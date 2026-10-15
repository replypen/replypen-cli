package render

import (
	"bytes"
	"testing"
)

func TestPathsGolden(t *testing.T) {
	paths := []string{"/brain/AGENTS.md", "/tenant/AGENTS.md", "/kb/knowledgeowl"}
	ok := func(tenant string, kinds ...string) PathsResult {
		r := PathsResult{Tenant: tenant, BrainResolved: "channel:stable @ abc123", RunIDs: []string{"run-" + tenant}}
		for i, k := range kinds {
			r.Entries = append(r.Entries, PathEntry{Path: paths[i], Kind: k})
		}
		return r
	}
	failed := PathsResult{Tenant: "broken", RunIDs: []string{}, Error: "remote check exited 2: sh: boom"}

	var wide bytes.Buffer
	Paths(&wide, PathsReport{Project: "alpha", Paths: paths, Results: []PathsResult{
		ok("acme", "file", "dir", "dir"), ok("beta", "file", "missing", "dir"), failed,
	}})
	assertGolden(t, "paths_wide.golden", wide.String())

	results := []PathsResult{failed}
	for _, tenant := range []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7"} {
		results = append(results, ok(tenant, "file", "missing", "dir"))
	}
	results[1].Entries[1].Kind = "file"
	var rollup bytes.Buffer
	Paths(&rollup, PathsReport{Project: "alpha", Paths: paths, Results: results})
	assertGolden(t, "paths_rollup.golden", rollup.String())
}

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/render"
)

// pathsChunkBudget bounds one remote check: both the script and its expected stdout stay under it. The
// binding server limits are tighter than maxConsoleInputBytes — the command rides docker-exec argv
// (120 KiB) and each stream is captured at 64 KiB — so 48 KiB keeps both with headroom.
const pathsChunkBudget = 48 << 10

// pathsPrelude is the POSIX helper every chunk starts with: one `<kind>\t<path>` line per argument.
// `-d`/`-e` follow symlinks, so a dangling link is "missing" — the same answer a reading run would get.
const pathsPrelude = "c(){ if [ -d \"$1\" ]; then k=dir; elif [ -e \"$1\" ]; then k=file; else k=missing; fi; printf '%s\\t%s\\n' \"$k\" \"$1\"; }\n"

func newConsolePathsCmd(e *env) *cobra.Command {
	var tenants string
	var concurrency int
	cmd := &cobra.Command{
		Use:   "paths [--tenants a,b|all] <path>...|-",
		Short: "Check which absolute paths exist in a production workspace",
		Long: "Reports dir/file/missing for each absolute path as a real production workspace sees it\n" +
			"(/brain, /tenant, /mirrors/<repo>, /kb/<provider>, …), optionally per tenant. Existence only:\n" +
			"surface-scoped docs are stubbed in the console, so file content there is not representative,\n" +
			"and /skills is not mounted in the console at all. A single - reads newline-separated paths from\n" +
			"stdin. Exits 4 when any path is missing or any checked tenant failed. Scripts reading -o json\n" +
			"should add --raw-output so a large report is never replaced by a spill manifest.",
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			paths, err := consolePathsInput(e, args)
			if err != nil {
				return err
			}
			if concurrency < 1 {
				return fmt.Errorf("--concurrency must be at least 1")
			}
			fanOut := cmd.Flags().Changed("tenants")
			var targets []string
			if fanOut {
				if e.tenant != "" || e.scope != "" {
					return fmt.Errorf("--tenants cannot be combined with --tenant or --scope")
				}
				if targets, err = parseTenantsFlag(tenants); err != nil {
					return err
				}
			}
			c, err := e.newClient()
			if err != nil {
				return err
			}
			report := render.PathsReport{Project: e.scopeProject(), Paths: paths}
			chunks := chunkPaths(paths)
			if !fanOut {
				// The normal scope: one target, and a request-level failure (TENANT_REQUIRED, auth) is
				// surfaced verbatim instead of being folded into a result row.
				res, echoed, err := checkPathsTarget(e.ctx(), c, report.Project, e.scopeTenant(), chunks)
				if err != nil {
					return err
				}
				if report.Project == "" {
					report.Project = echoed
				}
				report.Results = []render.PathsResult{res}
			} else {
				if report.Project == "" {
					report.Project = e.resolved.Project
				}
				if report.Project == "" {
					if err := e.resolvePinnedProject(c); err != nil {
						return err
					}
					report.Project = e.scopeProject()
				}
				if len(targets) == 0 { // --tenants all
					if targets, err = listActiveTenants(e.ctx(), c, report.Project); err != nil {
						return err
					}
					if len(targets) == 0 {
						return fmt.Errorf("--tenants all: project %s has no active tenants", report.Project)
					}
				}
				report.Results = checkPathsFanOut(e.ctx(), c, report.Project, targets, chunks, concurrency)
			}
			if e.jsonOut() {
				raw, err := json.Marshal(report)
				if err != nil {
					return fmt.Errorf("encode paths report: %w", err)
				}
				if err := e.renderJSON("console-paths", raw); err != nil {
					return err
				}
			} else {
				render.Paths(e.out, report)
			}
			if !pathsAllPresent(report) {
				return pathsIncompleteError()
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&tenants, "tenants", "", "check each listed tenant (comma-separated), or all active tenants of the project")
	cmd.Flags().IntVar(&concurrency, "concurrency", 4, "max tenants checked in parallel")
	return cmd
}

// consolePathsInput validates and dedupes the path list (order preserved) before any request.
func consolePathsInput(e *env, args []string) ([]string, error) {
	raw := args
	if len(args) == 1 && args[0] == "-" {
		r := e.in
		if r == nil {
			r = os.Stdin
		}
		text, err := readConsoleInput(r, "stdin")
		if err != nil {
			return nil, err
		}
		raw = nil
		for _, line := range strings.Split(text, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				raw = append(raw, line)
			}
		}
	}
	seen := make(map[string]bool, len(raw))
	var paths []string
	for _, p := range raw {
		if p == "-" {
			return nil, fmt.Errorf("- (stdin) must be the only path argument")
		}
		if !strings.HasPrefix(p, "/") {
			return nil, fmt.Errorf("path %q is not absolute", p)
		}
		if strings.ContainsAny(p, "\x00\n\r") {
			return nil, fmt.Errorf("path %q contains a NUL or newline", p)
		}
		if !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no paths to check")
	}
	return paths, nil
}

// parseTenantsFlag returns nil for "all" (resolve the project's tenants later) or the deduped list.
func parseTenantsFlag(v string) ([]string, error) {
	if strings.TrimSpace(v) == "all" {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, t := range strings.Split(v, ",") {
		if t = strings.TrimSpace(t); t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--tenants needs a comma-separated tenant list or all")
	}
	return out, nil
}

// listActiveTenants is the `rp project tenant ls` call; archived tenants are skipped (they take no runs).
func listActiveTenants(ctx context.Context, c *client.Client, project string) ([]string, error) {
	list, _, err := c.List(ctx, "tenants", project, "")
	if err != nil {
		return nil, err
	}
	var out []string
	for _, it := range list.Items {
		var slug, status string
		if err := json.Unmarshal(it["slug"], &slug); err != nil || slug == "" {
			_ = json.Unmarshal(it["id"], &slug)
		}
		_ = json.Unmarshal(it["status"], &status)
		if slug != "" && status != "archived" {
			out = append(out, slug)
		}
	}
	return out, nil
}

// chunkPaths splits the list so each remote script and its stdout stay within pathsChunkBudget.
func chunkPaths(paths []string) [][]string {
	var chunks [][]string
	var cur []string
	size := len(pathsPrelude)
	for _, p := range paths {
		cost := max(len(pathsScriptLine(p)), len("missing\t")+len(p)+1)
		if len(cur) > 0 && size+cost > pathsChunkBudget {
			chunks = append(chunks, cur)
			cur, size = nil, len(pathsPrelude)
		}
		cur = append(cur, p)
		size += cost
	}
	if len(cur) > 0 {
		chunks = append(chunks, cur)
	}
	return chunks
}

func pathsScript(paths []string) string {
	var b strings.Builder
	b.WriteString(pathsPrelude)
	for _, p := range paths {
		b.WriteString(pathsScriptLine(p))
	}
	return b.String()
}

// pathsScriptLine passes the path as one single-quoted literal: inside '…' nothing expands, and an
// embedded ' is closed, escaped and reopened.
func pathsScriptLine(p string) string {
	return "c '" + strings.ReplaceAll(p, "'", `'\''`) + "'\n"
}

// parsePathsOutput is strict: every line must be a known kind for a requested path, and every requested
// path must answer exactly once. Anything else is an error, never a silent "missing".
func parsePathsOutput(chunk []string, stdout string) ([]render.PathEntry, error) {
	want := make(map[string]bool, len(chunk))
	for _, p := range chunk {
		want[p] = true
	}
	kinds := make(map[string]string, len(chunk))
	if body := strings.TrimSuffix(stdout, "\n"); body != "" {
		for _, line := range strings.Split(body, "\n") {
			kind, path, ok := strings.Cut(line, "\t")
			if !ok || (kind != "dir" && kind != "file" && kind != render.PathMissing) || !want[path] {
				return nil, fmt.Errorf("unexpected output line %q", line)
			}
			if _, dup := kinds[path]; dup {
				return nil, fmt.Errorf("duplicate output for %q", path)
			}
			kinds[path] = kind
		}
	}
	entries := make([]render.PathEntry, 0, len(chunk))
	for _, p := range chunk {
		kind, ok := kinds[p]
		if !ok {
			return nil, fmt.Errorf("no result for %q in remote output", p)
		}
		entries = append(entries, render.PathEntry{Path: p, Kind: kind})
	}
	return entries, nil
}

// checkPathsTarget runs every chunk for one scope, sequentially (one console session per scope). A
// transport/API error is returned; a failed or unparseable remote check lands in the result's Error.
// The second value is the server-echoed project, for a scope the caller left implicit.
func checkPathsTarget(ctx context.Context, c *client.Client, project, tenant string, chunks [][]string) (render.PathsResult, string, error) {
	res := render.PathsResult{Tenant: tenant, RunIDs: []string{}}
	echoed := ""
	for _, chunk := range chunks {
		resp, err := bashRunRetry(ctx, c, pathsScript(chunk), project, tenant)
		if err != nil {
			return res, echoed, err
		}
		if res.Tenant == "" {
			res.Tenant = resp.Tenant // the server-resolved (login/brain) tenant of the normal scope
		}
		if echoed == "" {
			echoed = resp.Project
		}
		if res.BrainResolved == "" {
			res.BrainResolved = resp.BrainResolved
		}
		if resp.RunID != "" && (len(res.RunIDs) == 0 || res.RunIDs[len(res.RunIDs)-1] != resp.RunID) {
			res.RunIDs = append(res.RunIDs, resp.RunID)
		}
		var failure string
		switch {
		case resp.TimedOut:
			failure = "remote check timed out"
		case resp.ExitCode != 0:
			failure = fmt.Sprintf("remote check exited %d", resp.ExitCode)
			if msg := strings.TrimSpace(resp.Stderr); msg != "" {
				failure += ": " + truncateRunes(msg, 300)
			}
		case resp.StdoutTruncated:
			failure = "remote output was truncated"
		}
		if failure == "" {
			entries, err := parsePathsOutput(chunk, resp.Stdout)
			if err != nil {
				failure = err.Error()
			}
			res.Entries = append(res.Entries, entries...)
		}
		if failure != "" {
			res.Error, res.Entries = failure, nil
			return res, echoed, nil
		}
	}
	return res, echoed, nil
}

// pathsRetryDelays paces re-sends after a gateway error. The transport never retries a console POST (it
// may write); the path script only reads, so re-sending it is safe and rides out a server recreate.
var pathsRetryDelays = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second}

func bashRunRetry(ctx context.Context, c *client.Client, script, project, tenant string) (*client.BashRunResponse, error) {
	for attempt := 0; ; attempt++ {
		resp, _, err := c.BashRun(ctx, client.BashRunRequest{Command: script}, project, tenant)
		var apiErr *client.APIError
		if err == nil || attempt == len(pathsRetryDelays) || !errors.As(err, &apiErr) || apiErr.Status < 502 || apiErr.Status > 504 {
			return resp, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(pathsRetryDelays[attempt]):
		}
	}
}

// checkPathsFanOut checks each tenant in parallel (bounded); every error becomes that tenant's row.
func checkPathsFanOut(ctx context.Context, c *client.Client, project string, tenants []string, chunks [][]string, concurrency int) []render.PathsResult {
	results := make([]render.PathsResult, len(tenants))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, tenant := range tenants {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res, _, err := checkPathsTarget(ctx, c, project, tenant, chunks)
			if err != nil {
				res.Error, res.Entries = err.Error(), nil
			}
			res.Tenant = tenant
			results[i] = res
		}()
	}
	wg.Wait()
	return results
}

func pathsAllPresent(r render.PathsReport) bool {
	for _, res := range r.Results {
		if res.Error != "" || len(res.Entries) != len(r.Paths) {
			return false
		}
		for _, entry := range res.Entries {
			if entry.Kind == render.PathMissing {
				return false
			}
		}
	}
	return true
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

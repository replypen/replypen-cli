package cli

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/replypen/replypen-cli/internal/debugdump"
	"github.com/replypen/replypen-cli/internal/outputspill"
	"github.com/replypen/replypen-cli/internal/render"
)

type phoneLabTarget struct{ project, runID, origin, tenant string }

func parsePhoneLabTarget(arg string) (phoneLabTarget, bool, error) {
	u, err := url.Parse(strings.TrimSpace(arg))
	if err != nil || !looksLikeURL(arg) {
		return phoneLabTarget{}, false, nil
	}
	segs := pathSegments(u)
	if len(segs) != 5 || segs[0] != "projects" || segs[2] != "phone-lab" || segs[3] != "runs" {
		return phoneLabTarget{}, false, nil
	}
	if u.Scheme != "https" && u.Scheme != "http" || u.Host == "" || u.User != nil || strings.ContainsAny(segs[1]+segs[4], "/\\") {
		return phoneLabTarget{}, true, fmt.Errorf("invalid phone-lab run URL")
	}
	return phoneLabTarget{project: segs[1], runID: segs[4], origin: origin(u), tenant: u.Query().Get("tenant")}, true, nil
}

func runPhoneLabDebug(e *env, target phoneLabTarget, outDir string) error {
	if e.project != "" && target.project != "" && e.project != target.project {
		return fmt.Errorf("--project %s conflicts with phone-lab URL project %s", e.project, target.project)
	}
	if target.project != "" {
		e.project = target.project
	}
	if target.tenant != "" {
		if e.scope == scopeSelectorProject || e.tenant != "" && e.tenant != target.tenant {
			return fmt.Errorf("tenant selector conflicts with phone-lab URL tenant %s", target.tenant)
		}
		e.tenant = target.tenant
	}
	// A pasted URL cannot redirect an OAuth bearer. Resolve the trusted config before any client I/O.
	res, err := e.loadConfig()
	if err != nil {
		return err
	}
	base := res.BaseURL
	if e.baseURLOvr != "" {
		base = e.baseURLOvr
	}
	if target.origin != "" && target.origin != strings.TrimRight(base, "/") {
		return fmt.Errorf("phone-lab URL origin differs from configured API (%s); set REPLYPEN_BASE_URL explicitly", base)
	}
	c, err := e.newClient()
	if err != nil {
		return err
	}
	project := e.scopeProject()
	if project == "" {
		return fmt.Errorf("phone-lab id requires --project or a phone-lab URL")
	}
	// A pasted run already identifies its tenant; ambient brain overlays are not additional selectors.
	tenant := e.tenant
	if e.scope == scopeSelectorTenant {
		tenant = e.scopeTenant()
	}
	e.resolved.Tenant, e.loginTenant = tenant, ""
	detail, raw, err := c.PhoneLabRunWithRaw(e.ctx(), project, target.runID, tenant)
	if err != nil {
		return err
	}
	if outDir == "" {
		outDir = defaultDebugDir
	}
	if err := mkdirLocal(outDir, 0o755); err != nil {
		return err
	}
	// The UUID is server-authored; project/tenant labels never become filesystem components.
	id := detail.RunID
	if id != target.runID || strings.ContainsAny(id, "/\\") || id == "" {
		return fmt.Errorf("phone-lab response run_id does not match request")
	}
	baseName := "phone-" + id
	indexPath, rawPath := filepath.Join(outDir, baseName+".md"), filepath.Join(outDir, baseName+".json")
	eventsPath := filepath.Join(outDir, baseName+".jsonl")
	events, err := debugdump.PhoneLabJSONL(raw)
	if err != nil {
		return err
	}
	if err := os.WriteFile(eventsPath, events, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(rawPath, raw, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(indexPath, []byte(debugdump.PhoneLabIndex(detail)), 0o600); err != nil {
		return err
	}
	if e.jsonOut() && !e.rawOutput {
		cfg := e.spillConfig()
		index, err := outputspill.ArtifactForFile(cfg, indexPath, "text", false)
		if err != nil {
			return err
		}
		trace, err := outputspill.ArtifactForFile(cfg, rawPath, "json", false)
		if err != nil {
			return err
		}
		eventArtifact, err := outputspill.ArtifactForFile(cfg, eventsPath, "jsonl", false)
		if err != nil {
			return err
		}
		return outputspill.WriteManifest(e.out, outputspill.Manifest{Spilled: true, Artifacts: map[string]outputspill.Artifact{"index": index, "trace": trace, "events": eventArtifact}, Hints: []string{"cat " + outputspill.ShellQuote(index.Path), "jq 'select(.type==\"event\" and .event.kind==\"model\")' " + outputspill.ShellQuote(eventArtifact.Path)}, RawModeHint: "rerun with --raw-output to print paths"})
	}
	_, _ = fmt.Fprintln(e.out, indexPath)
	_, _ = fmt.Fprintln(e.out, rawPath)
	_, _ = fmt.Fprintln(e.out, eventsPath)
	_, _ = fmt.Fprintf(e.err, "phone-lab %s · status=%s · %d events · read index, then jq .events[INDEX]\n", detail.Project, detail.Status, len(detail.Events))
	return nil
}

func runPhoneLabList(e *env, limit int) error {
	c, err := e.newClient()
	if err != nil {
		return err
	}
	project := e.scopeProject()
	if project == "" {
		return fmt.Errorf("phone-lab listing requires --project or a brain project")
	}
	// Explicit tenant is a filter under project developer authority, not a tenant-token route.
	tenant := e.tenant
	if e.scope == scopeSelectorTenant {
		tenant = e.scopeTenant()
	}
	resp, raw, err := c.PhoneLabRuns(e.ctx(), project, tenant, limit)
	if err != nil {
		return err
	}
	if e.jsonOut() {
		return e.renderJSONInline(raw)
	}
	render.PhoneLabRuns(e.out, resp, project)
	return nil
}

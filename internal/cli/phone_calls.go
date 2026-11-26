package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/digest"
	"github.com/replypen/replypen-cli/internal/render"
)

// `rp run calls` / `rp run call` are the review rungs over the phone-lab run store (project developer
// access, the same endpoints as `run list --phone-lab` and `run debug --phone-lab`): list → one call as a
// dialogue → --steps → `run debug` for the forensic artifacts. Outcomes are computed client-side from each
// run's events (digest.DigestPhoneCall), so the list fetches every listed run's detail.

const phoneCallFetches = 6

func newRunCallsCmd(e *env) *cobra.Command {
	var limit int
	var since string
	cmd := &cobra.Command{
		Use:   "calls",
		Short: "List phone calls, newest first: when, caller, length, outcome (drill: rp run call <run>)",
		Long: "List a phone project's calls (phone-lab runs), newest first: local time, caller (last 3 digits), length, " +
			"caller turns, outcome (booked|moved|cancelled|message|waitlist, else no action or hung up) and the short run id.\n\n" +
			"Drill down: rp run call <run> reads one call as a dialogue; add --steps for the script calls, -o json for the raw run, " +
			"rp run debug <run> --phone-lab for the full forensic dump. Needs project developer access; --tenant narrows to one tenant.",
		Example: "  rp run calls --project dentai-phone-nl --since 24h\n  rp run call d823f768 --project dentai-phone-nl --steps",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if limit < 1 || limit > 100 {
				return fmt.Errorf("--limit must be 1..100")
			}
			var from time.Time
			if since != "" {
				t, err := parseCallsSince(since, nowFunc(), time.Local)
				if err != nil {
					return err
				}
				from = t
			}
			c, project, tenant, err := phoneCallScope(e)
			if err != nil {
				return err
			}
			fetch := limit
			if !from.IsZero() {
				fetch = 100 // the endpoint has no time filter or pagination: take its maximum page, cut locally
			}
			list, raw, err := c.PhoneLabRuns(e.ctx(), project, tenant, fetch)
			if err != nil {
				return err
			}
			var runs []client.PhoneLabRun
			for _, r := range list.Items {
				if !from.IsZero() && r.CreatedAt.Before(from) {
					continue
				}
				if len(runs) == limit {
					break
				}
				runs = append(runs, r)
			}
			calls, bodies, err := fetchPhoneCalls(e, c, project, runs)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				return writePhoneCallsJSON(e, calls, runs, raw, bodies)
			}
			render.PhoneCalls(e.out, calls, project, tenant, time.Local)
			return nil
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "max calls (1..100)")
	cmd.Flags().StringVar(&since, "since", "", "only calls since: a duration (90m, 24h, 7d), a local date (2026-11-26) or RFC3339")
	return cmd
}

func newRunCallCmd(e *env) *cobra.Command {
	var steps bool
	cmd := &cobra.Command{
		Use:   "call <run>",
		Short: "Read one phone call as a dialogue with its outcome (--steps: script calls)",
		Long: "Print one call: agent and caller turns with their time into the call, then what the call changed. " +
			"<run> is the run id or the 8-character short id `rp run calls` prints. --steps interleaves each script call " +
			"(script, arguments, its stdout status); -o json prints the raw run (all events) for machines. " +
			"Audio is not on the host: labs/phone/sip/pull_calls.py in the replypen repo pulls the Twilio recording.",
		Example: "  rp run call d823f768 --project dentai-phone-nl\n  rp run call d823f768 --project dentai-phone-nl --steps",
		Args:    cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, project, tenant, err := phoneCallScope(e)
			if err != nil {
				return err
			}
			id, err := resolvePhoneRunID(e, c, project, tenant, normalizeRunID(args[0]))
			if err != nil {
				return err
			}
			run, raw, err := c.PhoneLabRunWithRaw(e.ctx(), project, id, tenant)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				return e.renderJSON("phone-call-"+shortRunID(id), raw)
			}
			render.PhoneCall(e.out, digest.DigestPhoneCall(run, steps), run.Project, run.Tenant, steps, time.Local)
			return nil
		},
	}
	cmd.Flags().BoolVar(&steps, "steps", false, "interleave the script calls (tool, arguments, stdout status)")
	return cmd
}

func phoneCallScope(e *env) (*client.Client, string, string, error) {
	c, err := e.newClient()
	if err != nil {
		return nil, "", "", err
	}
	project := e.scopeProject()
	if project == "" {
		return nil, "", "", fmt.Errorf("phone calls need --project (e.g. --project dentai-phone-nl) or a phone brain checkout")
	}
	// Explicit tenant is a filter under project developer authority, not a tenant-token route.
	tenant := e.tenant
	switch e.scope {
	case scopeSelectorProject:
		tenant = ""
	case scopeSelectorTenant:
		tenant = e.scopeTenant()
	}
	return c, project, tenant, nil
}

// resolvePhoneRunID expands a short id against the newest 100 runs: the phone-lab detail route takes full
// ids only. Ambiguity is an error, never a guess.
func resolvePhoneRunID(e *env, c *client.Client, project, tenant, id string) (string, error) {
	if fullUUID.MatchString(id) {
		return id, nil
	}
	if len(id) < 4 || strings.Trim(id, "0123456789abcdef-") != "" {
		return "", fmt.Errorf("%q is not a run id (want the uuid or its short prefix from `rp run calls`)", id)
	}
	list, _, err := c.PhoneLabRuns(e.ctx(), project, tenant, 100)
	if err != nil {
		return "", err
	}
	var hits []string
	for _, r := range list.Items {
		if strings.HasPrefix(r.RunID, id) {
			hits = append(hits, r.RunID)
		}
	}
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("no call %s among the newest 100 in %s; pass the full run id", id, project)
	case 1:
		return hits[0], nil
	}
	return "", fmt.Errorf("%s is ambiguous: %s", id, strings.Join(hits, ", "))
}

// fetchPhoneCalls reads each run's detail (bounded parallelism) and digests it, keeping the list order.
func fetchPhoneCalls(e *env, c *client.Client, project string, runs []client.PhoneLabRun) ([]digest.PhoneCall, []json.RawMessage, error) {
	calls := make([]digest.PhoneCall, len(runs))
	bodies := make([]json.RawMessage, len(runs))
	errs := make([]error, len(runs))
	sem := make(chan struct{}, phoneCallFetches)
	var wg sync.WaitGroup
	for i, r := range runs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			detail, body, err := c.PhoneLabRunWithRaw(e.ctx(), project, r.RunID, "")
			if err != nil {
				errs[i] = fmt.Errorf("call %s: %w", shortRunID(r.RunID), err)
				return
			}
			calls[i], bodies[i] = digest.DigestPhoneCall(detail, false), body
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, nil, err
		}
	}
	return calls, bodies, nil
}

type phoneCallJSON struct {
	RunID     string                `json:"run_id"`
	Tenant    string                `json:"tenant,omitempty"`
	StartedAt time.Time             `json:"started_at"`
	EndedAt   *time.Time            `json:"ended_at,omitempty"`
	DurationS int                   `json:"duration_s"`
	Caller    string                `json:"caller"`
	Turns     int                   `json:"caller_turns"`
	Outcome   string                `json:"outcome"`
	Outcomes  []digest.PhoneOutcome `json:"outcomes"`
}

// writePhoneCallsJSON: a computed report (rule b): our rows plus, under raw, the verbatim list body and every
// run detail body the outcomes were digested from (keyed by run id).
func writePhoneCallsJSON(e *env, calls []digest.PhoneCall, runs []client.PhoneLabRun, raw json.RawMessage, bodies []json.RawMessage) error {
	rows := make([]phoneCallJSON, 0, len(calls))
	for i, c := range calls {
		outcomes := c.Outcomes
		if outcomes == nil {
			outcomes = []digest.PhoneOutcome{}
		}
		rows = append(rows, phoneCallJSON{RunID: c.RunID, Tenant: runs[i].Tenant, StartedAt: c.StartedAt, EndedAt: runs[i].EndedAt,
			DurationS: int(c.Duration.Seconds()), Caller: c.Caller, Turns: c.Turns, Outcome: c.Outcome(), Outcomes: outcomes})
	}
	raw, err := keepPhoneRuns(raw, runs)
	if err != nil {
		return err
	}
	details := make(map[string]json.RawMessage, len(runs))
	for i, r := range runs {
		details[r.RunID] = bodies[i]
	}
	b, err := json.Marshal(map[string]any{"calls": rows, "raw": map[string]any{"phone_lab_runs": raw, "phone_lab_run_details": details}})
	if err != nil {
		return err
	}
	return e.renderJSON("phone-calls", b)
}

// keepPhoneRuns drops the list body's rows a --since/--limit cut removed; the kept rows stay verbatim.
func keepPhoneRuns(raw json.RawMessage, runs []client.PhoneLabRun) (json.RawMessage, error) {
	var body map[string]json.RawMessage
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body["items"], &items); err != nil {
		return nil, err
	}
	keep := map[string]bool{}
	for _, r := range runs {
		keep[r.RunID] = true
	}
	kept := []json.RawMessage{}
	for _, it := range items {
		var row struct {
			RunID string `json:"run_id"`
		}
		if json.Unmarshal(it, &row) == nil && keep[row.RunID] {
			kept = append(kept, it)
		}
	}
	b, err := json.Marshal(kept)
	if err != nil {
		return nil, err
	}
	body["items"] = b
	return json.Marshal(body)
}

// parseCallsSince: a Go duration or Nd back from now, a YYYY-MM-DD local midnight, or RFC3339.
func parseCallsSince(s string, now time.Time, loc *time.Location) (time.Time, error) {
	if strings.HasSuffix(s, "d") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "d")); err == nil && n >= 0 {
			return now.AddDate(0, 0, -n), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d >= 0 {
		return now.Add(-d), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		return t, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid --since %q (want 90m, 24h, 7d, 2026-11-26 or RFC3339)", s)
}

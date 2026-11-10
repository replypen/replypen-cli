package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/render"
)

// maxReplayBatch caps one batch submit (ids × reps) and one `rp run wait`: an A/B round is tens of runs.
// Replays queue behind live work (River priority) on a box-wide worker cap, so the batch never starves mail.
const maxReplayBatch = 200

// replaySubmission is one row of a batch replay's output: which stored run, which repetition, which new run.
type replaySubmission struct {
	ReplayOf string `json:"replay_of"`
	Rep      int    `json:"rep"`
	RunID    string `json:"run_id,omitempty"`
	Status   string `json:"status"`
	Error    string `json:"error,omitempty"`
}

// submitReplayBatch submits every (id, rep) as its own replay and prints one row each. A failed submit is
// a row with status "error", not an abort: the caller keeps the run ids that did start.
func submitReplayBatch(e *env, c *client.Client, base client.SubmitRequest, ids []string, reps int, jsonMode bool) error {
	out := make([]replaySubmission, 0, len(ids)*reps)
	failed := 0
	for _, raw := range ids {
		// Resolve once per id: a short id becomes the full one, an unknown/foreign run fails all its reps.
		replayOf, resolveErr := strings.TrimSpace(raw), error(nil)
		target, err := parseRunTarget(raw, "")
		if err == nil {
			var orig *client.RunDetail
			if orig, err = c.Run(e.ctx(), target.runID, e.scopeProject(), e.scopeTenant()); err == nil {
				replayOf = orig.RunID
			}
		}
		resolveErr = err
		for rep := range reps {
			row := replaySubmission{ReplayOf: replayOf, Rep: rep}
			err := resolveErr
			if err == nil {
				req := base
				req.ReplayRun = replayOf
				var sub *client.SubmitResponse
				if sub, _, err = c.Submit(e.ctx(), req); err == nil {
					row.RunID, row.Status = sub.RunID, sub.Status
				}
			}
			if err != nil {
				row.Status, row.Error = "error", err.Error()
				failed++
			}
			out = append(out, row)
		}
	}
	if jsonMode {
		body, err := json.Marshal(out)
		if err != nil {
			return err
		}
		if err := e.renderJSONInline(body); err != nil {
			return err
		}
	} else {
		for _, r := range out {
			_, _ = fmt.Fprintf(e.out, "%s\t%s\trep%d\t%s\n", r.RunID, r.ReplayOf, r.Rep, firstNonEmpty(r.Error, r.Status))
		}
		_, _ = fmt.Fprintln(e.err, "submitted — wait with: rp run wait <run ids>")
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d replay submits failed", failed, len(out))
	}
	return nil
}

// newRunWaitCmd: block until every given run is terminal (or --timeout), polling them concurrently from one
// process. --once reads current statuses and returns. JSON mode is an array of the verbatim GET /runs/{id}
// bodies (what `rp run show -o json` prints), so a batch caller stores each run exactly as a single ask would.
func newRunWaitCmd(e *env) *cobra.Command {
	var timeout time.Duration
	var once bool
	cmd := &cobra.Command{
		Use:   "wait <id>...",
		Short: "Wait for many runs at once (e.g. a batch replay); --once just reads their status",
		Long: "Poll every given run until all reach a terminal status (done, failed, …) or --timeout elapses, then " +
			"print each one. -o json prints an array of the same bodies `rp run show -o json` returns. A run still " +
			"in flight at the timeout is printed as it stands and the command exits non-zero. --once reads each " +
			"status one time and returns (exit 0), for a caller that keeps its own loop.",
		Args: cobra.RangeArgs(1, maxReplayBatch),
		RunE: func(_ *cobra.Command, args []string) error {
			c, err := e.newClient()
			if err != nil {
				return err
			}
			ids := make([]string, len(args))
			for i, a := range args {
				ids[i] = normalizeRunID(a)
			}
			ctx, cancel := context.WithTimeout(e.ctx(), timeout)
			defer cancel()
			raws := make([]json.RawMessage, len(ids))
			details := make([]*client.RunDetail, len(ids))
			errs := make([]error, len(ids))
			sem := make(chan struct{}, 8)
			var wg sync.WaitGroup
			for i, id := range ids {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for {
						sem <- struct{}{}
						d, raw, err := c.RunWithRaw(ctx, id, e.scopeProject(), e.scopeTenant())
						<-sem
						if err == nil {
							details[i], raws[i], errs[i] = d, raw, nil
						} else if details[i] == nil {
							errs[i] = err // keep the last good read over a timeout/blip error
						}
						if once || ctx.Err() != nil || (err == nil && isTerminalStatus(d.Status)) || (err != nil && !isTransient(err)) {
							return
						}
						select {
						case <-ctx.Done():
							return
						case <-time.After(5 * time.Second):
						}
					}
				}()
			}
			wg.Wait()
			pending := 0
			for i := range ids {
				if details[i] == nil || !isTerminalStatus(details[i].Status) {
					pending++
				}
			}
			if render.IsJSON(e.mode(), e.out) {
				items := make([]json.RawMessage, 0, len(ids))
				for i, id := range ids {
					switch {
					case raws[i] != nil:
						items = append(items, raws[i])
					default:
						msg := "unknown"
						if errs[i] != nil {
							msg = errs[i].Error()
						}
						b, _ := json.Marshal(map[string]string{"run_id": id, "status": "error", "error": msg})
						items = append(items, b)
					}
				}
				body, err := json.Marshal(items)
				if err != nil {
					return err
				}
				if err := e.renderJSONInline(body); err != nil {
					return err
				}
			} else {
				for i, id := range ids {
					switch {
					case details[i] != nil:
						_, _ = fmt.Fprintf(e.out, "%s\t%s\t%s\n", id, details[i].Status, details[i].Outcome)
					case errs[i] != nil:
						_, _ = fmt.Fprintf(e.out, "%s\terror\t%s\n", id, errs[i].Error())
					}
				}
			}
			if pending > 0 && !once {
				return fmt.Errorf("%d of %d runs not terminal after %s", pending, len(ids), timeout)
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "max time to wait for every run")
	cmd.Flags().BoolVar(&once, "once", false, "read each run's status once and return")
	return cmd
}

// isTransient: a network blip or 5xx while polling is retried on the next tick; a 404/403 is final.
func isTransient(err error) bool {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status >= 500 || apiErr.Status == 429
	}
	return true
}

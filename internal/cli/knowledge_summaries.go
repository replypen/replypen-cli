package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/client"
)

// newKnowledgeSummariesCmd replaces the host's enriched one-line article summaries (what grounding picks
// KB articles by) with reviewed ones. The kit's brain-description-audit skill writes the patch file.
func newKnowledgeSummariesCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "summaries",
		Short: "Replace synced KB article summaries with reviewed ones",
	}
	cmd.AddCommand(knowledgeSummariesApplyCmd(e))
	return cmd
}

func knowledgeSummariesApplyCmd(e *env) *cobra.Command {
	var from string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "apply --from <patches.json>",
		Short: "Check reviewed summaries against the synced KB and queue the sync that applies them",
		Long: "Replace synced KB article summaries with reviewed ones.\n\n" +
			"--from is JSON: {\"patches\": [{\"provider\", \"id\", \"body_hash\", \"summary\"}, …]} (or a bare array),\n" +
			"with id and body_hash copied from the article's synced frontmatter. Each patch is checked: ok,\n" +
			"stale (the article changed since you judged it), not_found or invalid (one line, ≤200 chars).\n" +
			"The ok ones are applied by a KB sync queued for this scope; later syncs keep them until the\n" +
			"article body changes. --tenant targets the tenant's own KB. A real apply needs the\n" +
			"`knowledge:write` token scope; --dry-run works with read.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			patches, err := readSummaryPatches(from)
			if err != nil {
				return err
			}
			c, err := e.newClient()
			if err != nil {
				return err
			}
			if err := e.resolvePinnedProject(c); err != nil {
				return err
			}
			resp, raw, err := c.KnowledgeSummariesApply(e.ctx(), e.scopeProject(), e.scopeTenant(),
				client.KnowledgeSummariesRequest{Patches: patches, DryRun: dryRun})
			if err != nil {
				return err
			}
			if e.jsonOut() {
				return e.renderJSONInline(raw)
			}
			counts := map[string]int{}
			for _, r := range resp.Results {
				counts[r.Status]++
				if r.Status != "ok" {
					_, _ = fmt.Fprintf(e.out, "%-9s %s/%s %s\n", r.Status, r.Provider, r.ID, r.Reason)
				}
			}
			keys := make([]string, 0, len(counts))
			for k := range counts {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, k := range keys {
				parts = append(parts, fmt.Sprintf("%s %d", k, counts[k]))
			}
			verb := "queued"
			if resp.DryRun {
				verb = "would queue"
			}
			_, _ = fmt.Fprintf(e.out, "%s · %s %d", strings.Join(parts, " · "), verb, resp.Queued)
			if resp.JobID != "" {
				_, _ = fmt.Fprintf(e.out, " (kb sync job %s)", resp.JobID)
			}
			_, _ = fmt.Fprintln(e.out)
			return nil
		},
	}
	cmd.Flags().StringVar(&from, "from", "", "JSON patch file")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "check the patches without queueing the sync")
	return cmd
}

func readSummaryPatches(from string) ([]client.KnowledgeSummaryPatch, error) {
	if from == "" {
		return nil, fmt.Errorf("--from <patches.json> is required")
	}
	b, err := os.ReadFile(from)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", from, err)
	}
	var wrapped struct {
		Patches []client.KnowledgeSummaryPatch `json:"patches"`
	}
	if err := json.Unmarshal(b, &wrapped); err != nil || wrapped.Patches == nil {
		if err := json.Unmarshal(b, &wrapped.Patches); err != nil {
			return nil, fmt.Errorf("%s: want {\"patches\": [...]} or a JSON array: %w", from, err)
		}
	}
	if len(wrapped.Patches) == 0 {
		return nil, fmt.Errorf("%s holds no patches", from)
	}
	return wrapped.Patches, nil
}

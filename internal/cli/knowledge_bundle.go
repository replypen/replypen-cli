package cli

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/config"
)

// newKnowledgeBundleCmd checks a site-published KB article bundle against the host's own parser, so a
// site can prove "ReplyPen will ingest this" before (or while) it goes live.
func newKnowledgeBundleCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "bundle", Short: "Check a site-published KB article bundle"}
	cmd.AddCommand(knowledgeBundleCheckCmd(e))
	return cmd
}

func knowledgeBundleCheckCmd(e *env) *cobra.Command {
	var fromURL, file string
	cmd := &cobra.Command{
		Use:   "check --url <bundle url> | --file <bundle.json>",
		Short: "Validate a KB bundle with the host's parser (non-zero exit on errors)",
		Long: "Runs the host's `bundle` KB provider parser on a bundle (schema replypen.kb-bundle/1) and lists\n" +
			"its errors (the sync would refuse the bundle) and warnings (synced, but a link or image will not\n" +
			"resolve). Nothing is stored. --url fetches from this machine, so a branch preview URL works.",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if (fromURL == "") == (file == "") {
				return errors.New("give exactly one of --url or --file")
			}
			bundle, err := readBundle(e, fromURL, file)
			if err != nil {
				return err
			}
			res, err := config.LoadFor(e.profile, e.project)
			if err != nil {
				return err
			}
			base := res.BaseURL
			if e.baseURLOvr != "" {
				base = e.baseURLOvr
			}
			out, raw, err := client.CheckKBBundle(e.ctx(), base, bundle)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				if err := e.renderJSONInline(raw); err != nil {
					return err
				}
			} else {
				for _, p := range out.Problems {
					where := ""
					if p.Article != "" {
						where = p.Article + ": "
					}
					_, _ = fmt.Fprintf(e.out, "%-7s %s%s\n", p.Level, where, p.Message)
				}
				verdict := "ok"
				if !out.OK {
					verdict = "REFUSED"
				}
				_, _ = fmt.Fprintf(e.out, "%s · %d articles · %d problems\n", verdict, out.Articles, len(out.Problems))
			}
			if !out.OK {
				// The verdict is already rendered (one JSON document in -o json); only the exit code is left.
				return &commandError{code: exitUsage, name: "BUNDLE_REFUSED", silent: true, message: "bundle refused: the KB sync would keep the previous articles"}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&fromURL, "url", "", "bundle URL (e.g. https://<preview>/help-center/replypen-kb.json)")
	cmd.Flags().StringVar(&file, "file", "", "bundle JSON file")
	return cmd
}

func readBundle(e *env, fromURL, file string) ([]byte, error) {
	if file != "" {
		return os.ReadFile(file)
	}
	req, err := http.NewRequestWithContext(e.ctx(), http.MethodGet, fromURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: HTTP %d", fromURL, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBundleBytes+1))
	if err == nil && len(b) > maxBundleBytes {
		return nil, fmt.Errorf("fetch %s: bundle exceeds %d bytes (the host refuses it)", fromURL, maxBundleBytes)
	}
	return b, err
}

// maxBundleBytes mirrors the host's kbsync.MaxBundleBytes.
const maxBundleBytes = 20 << 20

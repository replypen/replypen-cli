package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/render"
)

// newReviewCmd wires `rp project review` — the bearer twin of the Review page (…/review). An agent that
// fixed a card's issue elsewhere retires it with `close`; `decide` records exactly what the page would.
func newReviewCmd(e *env) *cobra.Command {
	var audience string
	cmd := &cobra.Command{Use: "review", Short: "Work the review queue: list, show, decide, close or finish cards (like the Review page)"}
	cmd.PersistentFlags().StringVar(&audience, "audience", "", "owner|technical (default owner; show/decide/close try owner, then technical)")
	cmd.AddCommand(reviewListCmd(e, &audience), reviewShowCmd(e, &audience), reviewDecideCmd(e, &audience), reviewCloseCmd(e, &audience), reviewFinishCmd(e, &audience))
	return cmd
}

func reviewListCmd(e *env, audience *string) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List open review cards of one audience",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, project, err := reviewClient(e, *audience)
			if err != nil {
				return err
			}
			deck, raw, err := c.ReviewDeck(e.ctx(), project, e.scopeTenant(), *audience, "")
			if err != nil {
				return err
			}
			if e.jsonOut() {
				return e.renderJSON("review", raw)
			}
			render.ReviewCards(e.out, *deck)
			return nil
		},
	}
}

func reviewShowCmd(e *env, audience *string) *cobra.Command {
	return &cobra.Command{
		Use:   "show <card-id>",
		Short: "Show one review card (also once applied or closed) with its options and review-page link",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			c, project, err := reviewClient(e, *audience)
			if err != nil {
				return err
			}
			id, err := reviewCardID(args[0])
			if err != nil {
				return err
			}
			return reviewEachAudience(*audience, func(aud string) error {
				deck, raw, err := c.ReviewDeck(e.ctx(), project, e.scopeTenant(), aud, id)
				if err != nil {
					return err
				}
				var tiles struct {
					Items []json.RawMessage `json:"items"`
				}
				if err := json.Unmarshal(raw, &tiles); err != nil {
					return err
				}
				for n, card := range deck.Items {
					if card.ID != id {
						continue
					}
					if e.jsonOut() {
						return e.renderJSONInline(tiles.Items[n])
					}
					render.ReviewCard(e.out, card, reviewLink(c, deck.BaseURL, deck.Audience, id))
					return nil
				}
				return &client.APIError{Status: http.StatusNotFound, Code: "NOT_FOUND", Message: "no review card " + id + " in the " + aud + " review"}
			})
		},
	}
}

func reviewDecideCmd(e *env, audience *string) *cobra.Command {
	var label, instruction string
	cmd := &cobra.Command{
		Use:   "decide <card-id>",
		Short: "Decide a card: one of its option labels (see show), Other or Skip, plus an instruction",
		Long: "Records the decision exactly like the Review page. A decided queue card is picked up by the\n" +
			"review-implement run (Accept/Noise/Later close there; other labels become work). If the issue is\n" +
			"already fixed elsewhere, use `close` instead so no duplicate worker starts.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if strings.TrimSpace(label) == "" {
				return errors.New("--label is required (an option label from `rp project review show`, Other or Skip)")
			}
			c, project, err := reviewClient(e, *audience)
			if err != nil {
				return err
			}
			id, err := reviewCardID(args[0])
			if err != nil {
				return err
			}
			return reviewEachAudience(*audience, func(aud string) error {
				state, raw, err := c.ReviewDecide(e.ctx(), project, e.scopeTenant(), aud, id, label, instruction)
				if err != nil {
					return err
				}
				return reviewWritten(e, c, project, aud, state, raw)
			})
		},
	}
	cmd.Flags().StringVar(&label, "label", "", "decision label: one of the card's options, Other or Skip")
	cmd.Flags().StringVar(&instruction, "instruction", "", "instruction for the implementer (required for Other and on weekly reviews)")
	return cmd
}

func reviewCloseCmd(e *env, audience *string) *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "close <card-id>",
		Short: "Close a queue card already handled elsewhere, with a note saying where (commits, thread)",
		Long: "Records the decision \"Already handled\" and a done receipt carrying --note, in one step: the\n" +
			"card leaves the queue and the review-implement run never picks it up. If the issue is seen\n" +
			"again later, the publisher reopens it as a regression of that fix. Repeating the same close is safe.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if strings.TrimSpace(note) == "" {
				return errors.New("--note is required: say where the issue was handled (commits, thread, live date)")
			}
			c, project, err := reviewClient(e, *audience)
			if err != nil {
				return err
			}
			id, err := reviewCardID(args[0])
			if err != nil {
				return err
			}
			return reviewEachAudience(*audience, func(aud string) error {
				state, raw, err := c.ReviewClose(e.ctx(), project, e.scopeTenant(), aud, id, note)
				if err != nil {
					return err
				}
				return reviewWritten(e, c, project, aud, state, raw)
			})
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "where the issue was handled (required)")
	return cmd
}

func reviewFinishCmd(e *env, audience *string) *cobra.Command {
	return &cobra.Command{
		Use:   "finish",
		Short: "Finish the current review (a weekly review applies its decisions; the queue only acknowledges)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			c, project, err := reviewClient(e, *audience)
			if err != nil {
				return err
			}
			deck, _, err := c.ReviewDeck(e.ctx(), project, e.scopeTenant(), *audience, "")
			if err != nil {
				return err
			}
			if deck.ID == "" || deck.ID == "00000000-0000-0000-0000-000000000000" {
				return errors.New("no review session to finish")
			}
			if err := c.ReviewFinish(e.ctx(), project, e.scopeTenant(), *audience, deck.ID); err != nil {
				return err
			}
			if e.jsonOut() {
				raw, err := json.Marshal(map[string]any{"session_id": deck.ID, "cadence": deck.Cadence, "finished": true})
				if err != nil {
					return err
				}
				return e.renderJSONInline(raw)
			}
			_, _ = fmt.Fprintf(e.out, "finished %s review %s\n", deck.Cadence, deck.ID)
			return nil
		},
	}
}

func reviewClient(e *env, audience string) (*client.Client, string, error) {
	if audience != "" && audience != "owner" && audience != "technical" {
		return nil, "", fmt.Errorf("invalid --audience %q: owner or technical", audience)
	}
	c, err := e.newClient()
	if err != nil {
		return nil, "", err
	}
	project, err := pathProject(e, c, "review")
	if err != nil {
		return nil, "", err
	}
	// The review routes are the page's own handlers, which resolve {project} by name only.
	if fullUUID.MatchString(strings.ToLower(project)) {
		list, _, err := c.Projects(e.ctx())
		if err != nil {
			return nil, "", err
		}
		for _, p := range list.Projects {
			if strings.EqualFold(p.ID, project) {
				return c, p.Name, nil
			}
		}
		return nil, "", fmt.Errorf("project %s is not visible to this login", project)
	}
	return c, project, nil
}

// reviewCardID requires the full card id: cards are named in review links and receipts by full id.
func reviewCardID(arg string) (string, error) {
	id := strings.ToLower(strings.TrimSpace(arg))
	if !fullUUID.MatchString(id) {
		return "", fmt.Errorf("card id %q: give the full card id (ID column of `rp project review ls`, or the item= of a review link)", arg)
	}
	return id, nil
}

// reviewEachAudience runs fn for the explicit --audience, or tries owner then technical: a card id is
// unique, and a 404 means nothing was written. The owner error wins when technical is refused too.
func reviewEachAudience(audience string, fn func(string) error) error {
	if audience != "" {
		return fn(audience)
	}
	err := fn("owner")
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusNotFound {
		return err
	}
	if terr := fn("technical"); terr == nil || !isStatus(terr, http.StatusNotFound, http.StatusForbidden) {
		return terr
	}
	return err
}

func isStatus(err error, statuses ...int) bool {
	var apiErr *client.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	for _, s := range statuses {
		if apiErr.Status == s {
			return true
		}
	}
	return false
}

func reviewWritten(e *env, c *client.Client, project, audience string, state *client.ReviewItemState, raw json.RawMessage) error {
	if e.jsonOut() {
		return e.renderJSONInline(raw)
	}
	base := "/projects/" + project
	if tenant := e.scopeTenant(); tenant != "" {
		base += "/tenants/" + tenant
	}
	_, _ = fmt.Fprintf(e.out, "%s %s: %s\n%s\n", state.Status, state.ID, state.Label, reviewLink(c, base, audience, state.ID))
	return nil
}

// reviewLink is the card's deep link on the Review page.
func reviewLink(c *client.Client, base, audience, id string) string {
	if audience == "" {
		audience = "owner"
	}
	return strings.TrimRight(c.BaseURL(), "/") + base + "/weekly-review?" + url.Values{"audience": {audience}, "item": {id}}.Encode()
}

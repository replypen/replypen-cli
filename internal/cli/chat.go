package cli

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/replypen/replypen-cli/internal/client"
	"github.com/replypen/replypen-cli/internal/digest"
	"github.com/replypen/replypen-cli/internal/render"
)

func newChatCmd(e *env, version string) *cobra.Command {
	cmd := &cobra.Command{Use: "chat", Short: "Configure, diagnose, and smoke-test embedded chat"}
	cmd.AddCommand(newBagGetCmd(e, "/api/v1/chat"), newBagSetCmd(e, "/api/v1/chat"), chatSecretCmd(e), chatTokenCmd(e), chatSendCmd(e), chatReplayCmd(e), chatSessionCmd(e), chatCardDecideCmd(e), chatCardStatusCmd(e), chatFollowCmd(e), chatAttachmentCmd(e), chatDoctorCmd(e, version), chatBriefCmd(e))
	return cmd
}

func chatBriefCmd(e *env) *cobra.Command {
	var target, locale, scheme string
	cmd := &cobra.Command{Use: "brief", Short: "Print the secret-free chat implementation brief", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		if target != "bubble" && target != "page" {
			return fmt.Errorf("--target must be bubble or page")
		}
		if !slices.Contains([]string{"", "en", "nl", "fr"}, locale) {
			return fmt.Errorf("--locale must be en, nl, or fr")
		}
		if !slices.Contains([]string{"", "light", "dark"}, scheme) {
			return fmt.Errorf("--color-scheme must be light or dark")
		}
		c, err := e.newClient()
		if err != nil {
			return err
		}
		return c.ChatBrief(e.ctx(), e.scopeProject(), e.scopeTenant(), target, locale, scheme, e.out)
	}}
	cmd.Flags().StringVar(&target, "target", "bubble", "widget presentation: bubble or page")
	cmd.Flags().StringVar(&locale, "locale", "", "widget locale override: en, nl, or fr")
	cmd.Flags().StringVar(&scheme, "color-scheme", "", "widget color scheme override: light or dark")
	return cmd
}

func chatSecretCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "secret", Short: "Manage the dedicated chat signing secret"}
	for _, action := range []string{"rotate", "reveal"} {
		action := action
		label := strings.ToUpper(action[:1]) + action[1:]
		cmd.AddCommand(&cobra.Command{Use: action, Short: label + " the chat signing secret (printed once)", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
			c, err := e.newClient()
			if err != nil {
				return err
			}
			out, raw, err := c.ChatSecretAction(e.ctx(), e.scopeProject(), action)
			if err != nil {
				return err
			}
			if e.jsonOut() {
				return e.renderJSONInline(raw)
			}
			_, _ = fmt.Fprintln(e.out, out.Secret)
			if out.RotatedBy != "" && out.RotatedAt != "" {
				_, _ = fmt.Fprintf(e.err, "Rotated by %s at %s\n", out.RotatedBy, out.RotatedAt)
			}
			return nil
		}})
	}
	return cmd
}

func chatTokenCmd(e *env) *cobra.Command {
	var origin, kind, principalID string
	cmd := &cobra.Command{Use: "token", Short: "Mint a five-minute server chat token", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		c, err := e.newClient()
		if err != nil {
			return err
		}
		out, raw, err := c.ChatToken(e.ctx(), e.scopeProject(), client.ChatTokenRequest{
			Origin: origin, PrincipalKind: kind, ExternalID: principalID, Tenant: e.scopeTenant(),
		})
		if err != nil {
			return err
		}
		if e.jsonOut() {
			return e.renderJSONInline(raw)
		}
		_, _ = fmt.Fprintln(e.out, out.Token)
		return nil
	}}
	cmd.Flags().StringVar(&origin, "origin", "", "exact embedding-page origin")
	cmd.Flags().StringVar(&kind, "principal-kind", "", "declared principal kind")
	cmd.Flags().StringVar(&principalID, "principal-id", "", "principal external ID")
	_ = cmd.MarkFlagRequired("origin")
	return cmd
}

// chatLane is the --lane selector shared by `send` and `session`: "" keeps the embed plane (an embed
// token + origin), "setup"/"data" switch to the member dashboard plane over the OAuth bearer — the
// same conversation lists a logged-in member sees under "Set up & improve" / "Ask your data".
type chatLane struct {
	lane, intent, integration, action, tier string
}

func (l *chatLane) flags(cmd *cobra.Command, withOpen bool) {
	cmd.Flags().StringVar(&l.lane, "lane", "", "dashboard lane over the OAuth login instead of an embed token: setup (Set up & improve) or data (Ask your data)")
	if withOpen {
		cmd.Flags().StringVar(&l.intent, "intent", "", "seed the new --lane setup conversation like chat/new?intent=…: integration_setup, action_create, action_modify")
		cmd.Flags().StringVar(&l.integration, "integration", "", "connector key for --intent integration_setup (e.g. clickup)")
		cmd.Flags().StringVar(&l.action, "action", "", "action slug for --intent action_modify")
		cmd.Flags().StringVar(&l.tier, "tier", "", "session tier for a new --lane conversation: standard (Quick) or pro (Deeper)")
	}
}

func (l chatLane) dashboard() bool { return l.lane != "" }

func (l chatLane) validate() error {
	switch l.lane {
	case "", "setup", "data":
	default:
		return fmt.Errorf("--lane must be setup or data")
	}
	if l.intent != "" && l.lane != "setup" {
		return fmt.Errorf("--intent requires --lane setup")
	}
	if l.tier != "" && l.tier != "standard" && l.tier != "pro" {
		return fmt.Errorf("--tier must be standard or pro")
	}
	return nil
}

// open starts a dashboard conversation: an intent-seeded setup session, or a plain one in the lane.
func (l chatLane) open(e *env, c *client.Client, project, tenant string) (string, error) {
	if l.intent == "" {
		return c.DashboardChatOpen(e.ctx(), project, tenant, l.lane, l.tier)
	}
	params := url.Values{}
	if l.integration != "" {
		params.Set("integration", l.integration)
	}
	if l.action != "" {
		params.Set("action", l.action)
	}
	return c.DashboardChatOpenIntent(e.ctx(), project, tenant, l.intent, params)
}

func chatSendCmd(e *env) *cobra.Command {
	var token, origin, sessionID string
	var answerFlags []string
	var lane chatLane
	cmd := &cobra.Command{Use: "send [message]", Short: "Send one chat turn and print its SSE frames and session + run IDs", Args: cobra.MaximumNArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		if err := lane.validate(); err != nil {
			return err
		}
		if len(args) == 0 && len(answerFlags) == 0 {
			return fmt.Errorf("a message or at least one --answer key=value is required")
		}
		if sessionID == "" && len(answerFlags) > 0 {
			return fmt.Errorf("--session is required with --answer")
		}
		if sessionID != "" && lane.intent != "" {
			return fmt.Errorf("--intent opens a new conversation; drop --session")
		}
		plane, c, err := resolveChatPlane(e, lane, token, origin)
		if err != nil {
			return err
		}
		turn, sendErr := sendChatTurn(e, c, plane, sessionID, lane.tier, args, answerFlags, e.out)
		if turn.SessionID == "" {
			return sendErr
		}
		_, _ = fmt.Fprintf(e.out, "session_id: %s\n", turn.SessionID)
		if turn.RunID != "" {
			_, _ = fmt.Fprintf(e.out, "run_id: %s\n", turn.RunID)
		}
		if turn.Tier != "" {
			_, _ = fmt.Fprintf(e.out, "tier: %s\n", turn.Tier)
		}
		return sendErr
	}}
	cmd.Flags().StringVar(&token, "token", "", "embed chat token (default: RC_CHAT_TOKEN)")
	cmd.Flags().StringVar(&origin, "origin", "", "embedding-page origin (default: token origin claim)")
	cmd.Flags().StringVar(&sessionID, "session", "", "existing chat session ID (opens a new session when omitted)")
	cmd.Flags().StringArrayVar(&answerFlags, "answer", nil, "answer the latest data question as key=value (repeat for multiple answers)")
	lane.flags(cmd, true)
	return cmd
}

// chatPlane is one resolved chat target: the embed plane (token + origin + project, from flags or the
// token's own claims) or, with --lane, the member dashboard plane over the OAuth bearer, scoped by the
// brain/--project/--tenant exactly like the logged-in page.
type chatPlane struct {
	lane                           chatLane
	token, origin, project, tenant string
}

func resolveChatPlane(e *env, lane chatLane, token, origin string) (chatPlane, *client.Client, error) {
	p := chatPlane{lane: lane}
	if lane.dashboard() {
		p.project, p.tenant = e.scopeProject(), e.scopeTenant()
		if p.project == "" {
			return p, nil, fmt.Errorf("--lane needs a project: run inside a brain checkout or pass --project")
		}
	} else {
		var err error
		if p.token, p.origin, p.project, err = chatEmbedScope(e, token, origin); err != nil {
			return p, nil, err
		}
	}
	c, err := e.newClient()
	return p, c, err
}

// chatTurnResult is one sent turn; also the `-o json` row of `chat replay`.
type chatTurnResult = render.ChatTurn

// sendChatTurn is the ONE send path behind `send` and `replay`: opens a session when sessionID is "",
// builds the turn's parts, streams the SSE frames to out and returns the handles. tier is the member's
// pick (dashboard plane only; "" lets the host route). A returned SessionID with an error means the
// session exists and the turn failed in-stream.
func sendChatTurn(e *env, c *client.Client, p chatPlane, sessionID, tier string, args, answerFlags []string, out io.Writer) (chatTurnResult, error) {
	var err error
	if sessionID == "" {
		if p.lane.dashboard() {
			open := p.lane
			open.tier = tier
			sessionID, err = open.open(e, c, p.project, p.tenant)
		} else {
			sessionID, err = c.ChatOpen(e.ctx(), p.project, p.origin, p.token)
		}
		if err != nil {
			return chatTurnResult{}, err
		}
	}
	parts, err := chatTurnParts(args, answerFlags, func() (json.RawMessage, error) {
		if p.lane.dashboard() {
			return c.DashboardChatSession(e.ctx(), p.project, p.tenant, sessionID)
		}
		return c.ChatSession(e.ctx(), p.project, p.origin, p.token, sessionID)
	})
	if err != nil {
		return chatTurnResult{}, err
	}
	started := time.Now()
	var turn client.ChatTurn
	if p.lane.dashboard() {
		turn, err = c.DashboardChatSend(e.ctx(), p.project, p.tenant, sessionID, randomMessageID(), tier, parts, out)
	} else {
		turn, err = c.ChatSend(e.ctx(), p.project, p.origin, p.token, sessionID, randomMessageID(), parts, out)
	}
	res := chatTurnResult{SessionID: sessionID, RunID: turn.RunID, Tier: turn.Tier, Answer: turn.Answer, Secs: time.Since(started).Seconds()}
	if err != nil {
		res.Error = err.Error()
	}
	return res, err
}

// chatTurnParts assembles the typed parts of one turn: the text, plus a data-answers part built
// against the session's latest unanswered question set when --answer is given.
func chatTurnParts(args, answerFlags []string, transcript func() (json.RawMessage, error)) ([]map[string]any, error) {
	parts := make([]map[string]any, 0, 2)
	if len(args) == 1 && strings.TrimSpace(args[0]) != "" {
		parts = append(parts, map[string]any{"type": "text", "text": args[0]})
	}
	if len(answerFlags) > 0 {
		raw, err := transcript()
		if err != nil {
			return nil, err
		}
		answerPart, err := chatAnswerPart(raw, answerFlags)
		if err != nil {
			return nil, err
		}
		parts = append(parts, answerPart)
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("message must not be empty")
	}
	return parts, nil
}

// chatEmbedScope resolves the embed-plane triple every chat verb needs: the bearer (flag or
// RC_CHAT_TOKEN), the exact origin and the project, the latter two decoded from the token's own claims
// when not given — the token was minted for exactly one origin and one project.
func chatEmbedScope(e *env, token, origin string) (string, string, string, error) {
	if token == "" {
		token = os.Getenv("RC_CHAT_TOKEN")
	}
	if token == "" {
		return "", "", "", fmt.Errorf("--token or RC_CHAT_TOKEN is required")
	}
	if origin == "" {
		origin = jwtStringClaim(token, "origin")
	}
	if origin == "" {
		return "", "", "", fmt.Errorf("--origin is required when the token origin cannot be decoded")
	}
	project := e.scopeProject()
	if project == "" {
		project = jwtStringClaim(token, "iss")
	}
	if project == "" {
		return "", "", "", fmt.Errorf("--project is required when the token issuer cannot be decoded")
	}
	return token, origin, project, nil
}

// chatSessionCmd re-reads a session the way the widget does on reopen: the persisted transcript with
// every card hydrated to its current lifecycle state. Distinct from the live `send` stream — this is
// the read-after-park path (PII resolved from the sealed session vault, not the live container).
func chatSessionCmd(e *env) *cobra.Command {
	var token, origin string
	var lane chatLane
	cmd := &cobra.Command{Use: "session <id>", Short: "Print a session's persisted transcript as the widget would reopen it", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		if err := lane.validate(); err != nil {
			return err
		}
		if lane.dashboard() {
			c, err := e.newClient()
			if err != nil {
				return err
			}
			project := e.scopeProject()
			if project == "" {
				return fmt.Errorf("--lane needs a project: run inside a brain checkout or pass --project")
			}
			raw, err := c.DashboardChatSession(e.ctx(), project, e.scopeTenant(), strings.TrimSpace(args[0]))
			if err != nil {
				return err
			}
			return e.renderJSONInline(raw)
		}
		token, origin, project, err := chatEmbedScope(e, token, origin)
		if err != nil {
			return err
		}
		c, err := e.newClient()
		if err != nil {
			return err
		}
		raw, err := c.ChatSession(e.ctx(), project, origin, token, strings.TrimSpace(args[0]))
		if err != nil {
			return err
		}
		return e.renderJSONInline(raw)
	}}
	cmd.Flags().StringVar(&token, "token", "", "embed chat token (default: RC_CHAT_TOKEN)")
	cmd.Flags().StringVar(&origin, "origin", "", "embedding-page origin (default: token origin claim)")
	lane.flags(cmd, false)
	return cmd
}

// chatTarget is the plane every per-session verb shares (`decide`, `card`, `follow`, `attachment`):
// an embed token + origin, or --lane for the member dashboard plane over the OAuth bearer.
type chatTarget struct {
	token, origin string
	lane          chatLane
}

func (t *chatTarget) flags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&t.token, "token", "", "embed chat token (default: RC_CHAT_TOKEN)")
	cmd.Flags().StringVar(&t.origin, "origin", "", "embedding-page origin (default: token origin claim)")
	t.lane.flags(cmd, false)
}

// call resolves the plane and hands the matching call to embed or dashboard.
func (t *chatTarget) call(e *env,
	embed func(c *client.Client, project, origin, token string) error,
	dashboard func(c *client.Client, project, tenant string) error,
) error {
	if err := t.lane.validate(); err != nil {
		return err
	}
	if t.lane.dashboard() {
		project := e.scopeProject()
		if project == "" {
			return fmt.Errorf("--lane needs a project: run inside a brain checkout or pass --project")
		}
		c, err := e.newClient()
		if err != nil {
			return err
		}
		return dashboard(c, project, e.scopeTenant())
	}
	token, origin, project, err := chatEmbedScope(e, t.token, t.origin)
	if err != nil {
		return err
	}
	c, err := e.newClient()
	if err != nil {
		return err
	}
	return embed(c, project, origin, token)
}

// chatCardTarget adds the card route (--card, verbatim) to the plane. The CLI knows no per-card fields
// beyond the in-flight states `card --wait` stops on; the server validates the route and the outcome
// and returns the card projection, printed as JSON.
type chatCardTarget struct {
	chatTarget
	card string
}

func (t *chatCardTarget) flags(cmd *cobra.Command) {
	t.chatTarget.flags(cmd)
	cmd.Flags().StringVar(&t.card, "card", "website-changes", "card route: website-changes, actions, memories, brain-changes, outbound-batches")
}

// fetch runs the plane's call and returns the card projection.
func (t *chatCardTarget) fetch(e *env,
	embed func(c *client.Client, project, origin, token string) (json.RawMessage, error),
	dashboard func(c *client.Client, project, tenant string) (json.RawMessage, error),
) (json.RawMessage, error) {
	var raw json.RawMessage
	err := t.call(e, func(c *client.Client, project, origin, token string) (err error) {
		raw, err = embed(c, project, origin, token)
		return err
	}, func(c *client.Client, project, tenant string) (err error) {
		raw, err = dashboard(c, project, tenant)
		return err
	})
	return raw, err
}

// cardIDArg accepts a card as `send` prints its frame id (`bc_<uuid>`, `mem_<uuid>`, `ac_<uuid>`,
// `sch_<uuid>`, `ob_<uuid>`, …); the card routes take the bare uuid.
func cardIDArg(arg string) string {
	id := strings.TrimSpace(arg)
	if m := cardFrameIDRE.FindStringSubmatch(id); m != nil {
		return m[1]
	}
	return id
}

var cardFrameIDRE = regexp.MustCompile(`^[a-z]+_([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})$`)

// chatCardDecideCmd settles a chat card the way its widget button does (e.g. publish a website change).
func chatCardDecideCmd(e *env) *cobra.Command {
	var t chatCardTarget
	cmd := &cobra.Command{Use: "decide <session-id> <card-id> <outcome>", Short: "Settle a chat card (e.g. publish or decline a website change) and print its projection", Args: cobra.ExactArgs(3), RunE: func(_ *cobra.Command, args []string) error {
		sessionID, cardID, outcome := strings.TrimSpace(args[0]), cardIDArg(args[1]), strings.TrimSpace(args[2])
		raw, err := t.fetch(e, func(c *client.Client, project, origin, token string) (json.RawMessage, error) {
			return c.ChatCardDecide(e.ctx(), project, origin, token, sessionID, t.card, cardID, outcome)
		}, func(c *client.Client, project, tenant string) (json.RawMessage, error) {
			return c.DashboardChatCardDecide(e.ctx(), project, tenant, sessionID, t.card, cardID, outcome)
		})
		if err != nil {
			return err
		}
		return e.renderJSONInline(raw)
	}}
	t.flags(cmd)
	return cmd
}

// chatCardPollInterval paces `card --wait`; a var so tests poll without sleeping.
var chatCardPollInterval = 5 * time.Second

// chatCardStatusCmd reads a card's current projection — what the widget polls (e.g. while a website
// change builds). --wait polls until nothing is in flight (preview build, the host's review turn, the
// live build), so a script reads the state a person would act on.
func chatCardStatusCmd(e *env) *cobra.Command {
	var t chatCardTarget
	var wait bool
	var timeout time.Duration
	cmd := &cobra.Command{Use: "card <session-id> <card-id>", Short: "Print a chat card's current projection (--wait polls until it settles)", Args: cobra.ExactArgs(2), RunE: func(_ *cobra.Command, args []string) error {
		sessionID, cardID := strings.TrimSpace(args[0]), cardIDArg(args[1])
		ctx := e.ctx()
		if wait {
			// One deadline over every request, so a stalled poll cannot outlive --timeout.
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		read := func() (json.RawMessage, error) {
			return t.fetch(e, func(c *client.Client, project, origin, token string) (json.RawMessage, error) {
				return c.ChatCardStatus(ctx, project, origin, token, sessionID, t.card, cardID)
			}, func(c *client.Client, project, tenant string) (json.RawMessage, error) {
				return c.DashboardChatCardStatus(ctx, project, tenant, sessionID, t.card, cardID)
			})
		}
		var raw json.RawMessage
		last := ""
		for {
			next, err := read()
			if err != nil {
				if ctx.Err() == context.DeadlineExceeded {
					if raw == nil {
						return fmt.Errorf("card unread after %s: %w", timeout, err)
					}
					_ = e.renderJSONInline(raw)
					return fmt.Errorf("card still %s after %s", last, timeout)
				}
				return err
			}
			raw = next
			pending := digest.ChatCardPending(raw)
			if !wait || pending == "" {
				return e.renderJSONInline(raw)
			}
			if pending != last {
				_, _ = fmt.Fprintf(e.err, "waiting: %s\n", pending)
				last = pending
			}
			select {
			case <-ctx.Done():
				_ = e.renderJSONInline(raw)
				if ctx.Err() == context.DeadlineExceeded {
					return fmt.Errorf("card still %s after %s", pending, timeout)
				}
				return ctx.Err()
			case <-time.After(chatCardPollInterval):
			}
		}
	}}
	t.flags(cmd)
	cmd.Flags().BoolVar(&wait, "wait", false, "poll until nothing is in flight (preview build, review turn, live build)")
	cmd.Flags().DurationVar(&timeout, "timeout", 20*time.Minute, "with --wait: give up (non-zero exit) after this long")
	return cmd
}

// chatFollowCmd attaches to a run the user did not send — the host-started website review turn a card
// names in review_run_id — and prints its frames like `send`. A run that already finished prints
// nothing to follow; its answer is in `chat session`.
func chatFollowCmd(e *env) *cobra.Command {
	var t chatTarget
	cmd := &cobra.Command{Use: "follow <session-id> <run-id>", Short: "Stream a still-running turn (e.g. a card's review_run_id) like send", Args: cobra.ExactArgs(2), RunE: func(_ *cobra.Command, args []string) error {
		sessionID, runID := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
		var turn client.ChatTurn
		var settled bool
		err := t.call(e, func(c *client.Client, project, origin, token string) (err error) {
			turn, settled, err = c.ChatFollow(e.ctx(), project, origin, token, sessionID, runID, e.out)
			return err
		}, func(c *client.Client, project, tenant string) (err error) {
			turn, settled, err = c.DashboardChatFollow(e.ctx(), project, tenant, sessionID, runID, e.out)
			return err
		})
		if settled {
			_, _ = fmt.Fprintf(e.out, "run %s already settled; read its answer with `rp project chat session %s`\n", runID, sessionID)
			return nil
		}
		if turn.RunID != "" {
			_, _ = fmt.Fprintf(e.out, "run_id: %s\n", turn.RunID)
		}
		return err
	}}
	t.flags(cmd)
	return cmd
}

// chatAttachmentCmd downloads one file of the caller's own conversation — an upload, or a website
// card's review screenshot (its screenshots[].attachment_id). Admin reads of someone else's
// conversation are `rp run attachments`.
func chatAttachmentCmd(e *env) *cobra.Command {
	var t chatTarget
	var file string
	cmd := &cobra.Command{Use: "attachment <session-id> <attachment-id>", Short: "Download one file of your own conversation (e.g. a review screenshot)", Args: cobra.ExactArgs(2), RunE: func(_ *cobra.Command, args []string) error {
		sessionID, attachmentID := strings.TrimSpace(args[0]), strings.TrimSpace(args[1])
		if file == "" {
			file = attachmentID
		}
		// A temp sibling renamed on success: a failed download never clobbers an existing file.
		f, err := os.CreateTemp(filepath.Dir(file), "."+filepath.Base(file)+".*.part")
		if err != nil {
			return err
		}
		err = t.call(e, func(c *client.Client, project, origin, token string) error {
			return c.ChatAttachment(e.ctx(), project, origin, token, sessionID, attachmentID, f)
		}, func(c *client.Client, project, tenant string) error {
			return c.DashboardChatAttachment(e.ctx(), project, tenant, sessionID, attachmentID, f)
		})
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err == nil {
			err = os.Rename(f.Name(), file)
		}
		if err != nil {
			_ = os.Remove(f.Name())
			return err
		}
		_, _ = fmt.Fprintln(e.out, file)
		return nil
	}}
	t.flags(cmd)
	cmd.Flags().StringVar(&file, "file", "", "write the file here (default ./<attachment-id>)")
	return cmd
}

type chatQuestion struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

func chatAnswerPart(raw json.RawMessage, flags []string) (map[string]any, error) {
	var session struct {
		Messages []struct {
			Parts []json.RawMessage `json:"parts"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(raw, &session); err != nil {
		return nil, fmt.Errorf("decode chat session: %w", err)
	}
	answered := map[string]bool{}
	for _, message := range session.Messages {
		for _, rawPart := range message.Parts {
			var part struct {
				Type string `json:"type"`
				Data struct {
					QuestionSetID string `json:"question_set_id"`
				} `json:"data"`
			}
			if json.Unmarshal(rawPart, &part) == nil && part.Type == "data-answers" && part.Data.QuestionSetID != "" {
				answered[part.Data.QuestionSetID] = true
			}
		}
	}
	var questionSetID string
	var questions []chatQuestion
	for mi := len(session.Messages) - 1; mi >= 0 && questionSetID == ""; mi-- {
		parts := session.Messages[mi].Parts
		for pi := len(parts) - 1; pi >= 0; pi-- {
			var part struct {
				Type string `json:"type"`
				Data struct {
					QuestionSetID string         `json:"question_set_id"`
					Questions     []chatQuestion `json:"questions"`
				} `json:"data"`
			}
			if json.Unmarshal(parts[pi], &part) == nil && part.Type == "data-questions" && part.Data.QuestionSetID != "" && !answered[part.Data.QuestionSetID] {
				questionSetID, questions = part.Data.QuestionSetID, part.Data.Questions
				break
			}
		}
	}
	if questionSetID == "" {
		return nil, fmt.Errorf("session has no unanswered data question")
	}

	values := map[string][]string{}
	for _, flag := range flags {
		key, value, ok := strings.Cut(flag, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return nil, fmt.Errorf("--answer must be key=value")
		}
		values[key] = append(values[key], value)
	}
	byID := make(map[string]chatQuestion, len(questions))
	for _, q := range questions {
		byID[q.ID] = q
	}
	answers := map[string]any{}
	for id, selections := range values {
		q, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("unknown answer key %q", id)
		}
		switch q.Kind {
		case "single_select":
			if len(selections) != 1 {
				return nil, fmt.Errorf("answer %q accepts one value", id)
			}
			answers[id] = map[string]any{"value": selections[0]}
		case "multi_select":
			answers[id] = map[string]any{"values": selections}
		case "free_text":
			if len(selections) != 1 {
				return nil, fmt.Errorf("answer %q accepts one value", id)
			}
			answers[id] = map[string]any{"text": selections[0]}
		default:
			return nil, fmt.Errorf("question %q has unsupported kind %q", id, q.Kind)
		}
	}
	return map[string]any{"type": "data-answers", "data": map[string]any{"question_set_id": questionSetID, "answers": answers}}, nil
}

// doctorRejectLimit is how many recent rejects the doctor pulls — enough to see a pattern in the
// window, small enough to stay one page.
const doctorRejectLimit = 100

// chatDoctorJSON is the `-o json` / --bundle shape: request context, the config/secret bodies as decoded
// maps, then the diagnosis. Raw carries every fetched server body verbatim, keyed by source (SKILL.md
// "Output" rule b); it is set for `-o json` only, so the --bundle escalation artifact stays compact.
type chatDoctorJSON struct {
	Project    string                      `json:"project"`
	RCVersion  string                      `json:"rc_version"`
	Timestamp  time.Time                   `json:"timestamp"`
	Since      string                      `json:"since"`
	Config     map[string]any              `json:"config"`
	Principals digest.ChatDoctorPrincipals `json:"principals"`
	Secret     map[string]any              `json:"secret"`
	Branding   digest.ChatDoctorBranding   `json:"branding"`
	Rejects    []digest.ChatDoctorReject   `json:"rejects"`
	Probes     digest.ChatDoctorProbes     `json:"probes"`
	Findings   []digest.ChatDoctorFinding  `json:"findings"`
	Raw        map[string]json.RawMessage  `json:"raw,omitempty"`
}

func chatDoctorCmd(e *env, version string) *cobra.Command {
	var origin, kind, since string
	var bundle bool
	cmd := &cobra.Command{Use: "doctor", Short: "Diagnose embedded-chat configuration and recent rejects", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		if !slices.Contains([]string{"30m", "1h", "24h"}, since) {
			return fmt.Errorf("--since must be 30m, 1h, or 24h")
		}
		sinceDuration, _ := time.ParseDuration(since)
		c, err := e.newClient()
		if err != nil {
			return err
		}
		project := e.scopeProject()
		now := time.Now().UTC()
		out := chatDoctorJSON{Project: project, RCVersion: version, Timestamp: now, Since: since, Config: map[string]any{}, Secret: map[string]any{}}
		in := digest.ChatDoctorInputs{Origin: origin, PrincipalKind: kind, Since: sinceDuration}
		sources := map[string]json.RawMessage{}
		// A shape the doctor cannot decode is an error, never an empty section: empty rejects would read as a
		// clean bill of health.
		decodeDoctor := func(what string, raw json.RawMessage, dst any) error {
			if err := json.Unmarshal(raw, dst); err != nil {
				return fmt.Errorf("chat doctor: decode %s: %w", what, err)
			}
			return nil
		}
		// The report carries the bag as a decoded map; the checks read the SAME bytes through the typed
		// settings shape, so a field's Effective-then-Value ladder is decoded in one place.
		settings, raw, err := c.ChatSettings(e.ctx(), project)
		if err != nil {
			return err
		}
		if err := decodeDoctor("chat settings", raw, &out.Config); err != nil {
			return err
		}
		sources["chat_settings"], in.Settings = raw, *settings
		manifest, raw, err := c.Principals(e.ctx(), project)
		if err != nil {
			return err
		}
		sources["principals"], in.Principals = raw, *manifest
		raw, err = c.ChatSecretStatus(e.ctx(), project)
		if err != nil {
			return err
		}
		if err := decodeDoctor("chat secret status", raw, &out.Secret); err != nil {
			return err
		}
		sources["chat_secret"] = raw
		in.SecretSource, _ = out.Secret["source"].(string)
		// Branding is a diagnostic input, not a precondition: a doctor that aborts on it reports nothing
		// about the chat wiring the operator actually came to check.
		if branding, raw, err := c.GetBag(e.ctx(), "/api/v1/branding", project); err != nil {
			in.BrandingError = err.Error()
		} else {
			sources["branding"], in.Branding = raw, *branding
		}
		raw, err = c.ChatRejects(e.ctx(), project, doctorRejectLimit)
		if err != nil {
			return err
		}
		var rejects client.ChatRejectList
		if err := decodeDoctor("chat rejects", raw, &rejects); err != nil {
			return err
		}
		sources["chat_rejects"], in.Rejects = raw, rejects.Rejects
		in.OriginDNS = lookupOrigins(e.ctx(), digest.SettingStrings(in.Settings, "chat_origins"), net.DefaultResolver.LookupHost)
		var probeErr error
		in.WidgetLoaderStatus, probeErr = c.ProbeWidgetLoader(e.ctx())
		in.WidgetLoaderNetworkError = probeErr != nil

		report := digest.ChatDoctor(in, now)
		if bundle || e.jsonOut() {
			out.Principals, out.Branding, out.Rejects, out.Probes, out.Findings = report.Principals, report.Branding, report.Rejects, report.Probes, report.Findings
			if !bundle {
				out.Raw = sources
			}
			encoded, err := json.Marshal(out)
			if err != nil {
				return err
			}
			if err := e.renderJSONInline(encoded); err != nil {
				return err
			}
		} else {
			render.ChatDoctor(e.out, report)
		}
		if report.Failed {
			return &commandError{code: exitUsage, name: "CHAT_DOCTOR_FAILED", silent: true, message: "chat doctor found failures"}
		}
		return nil
	}}
	cmd.Flags().StringVar(&origin, "origin", "", "origin to check against the allowlist")
	cmd.Flags().StringVar(&kind, "principal-kind", "", "principal kind to check")
	cmd.Flags().BoolVar(&bundle, "bundle", false, "print a compact JSON report for escalation, without raw server bodies")
	cmd.Flags().StringVar(&since, "since", "24h", "reject warning window: 30m, 1h, or 24h")
	return cmd
}

func newPrincipalsCmd(e *env) *cobra.Command {
	cmd := &cobra.Command{Use: "principals", Short: "Read or replace the project principal manifest"}
	cmd.AddCommand(&cobra.Command{Use: "get", Short: "Show the principal manifest", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		c, err := e.newClient()
		if err != nil {
			return err
		}
		_, raw, err := c.Principals(e.ctx(), e.scopeProject())
		if err != nil {
			return err
		}
		return e.renderJSONInline(raw)
	}})
	cmd.AddCommand(&cobra.Command{Use: "set <json-or-yaml-file>", Short: "Replace the validated principal manifest", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		data, err := readCLIFile(e.in, args[0])
		if err != nil {
			return err
		}
		var body map[string]any
		if json.Unmarshal(data, &body) != nil {
			if err := yaml.Unmarshal(data, &body); err != nil {
				return fmt.Errorf("decode principal manifest: %w", err)
			}
		}
		c, err := e.newClient()
		if err != nil {
			return err
		}
		raw, err := c.SetPrincipals(e.ctx(), e.scopeProject(), body)
		if err != nil {
			return err
		}
		return e.renderJSONInline(raw)
	}})
	var kind, email, externalID string
	resolve := &cobra.Command{Use: "resolve", Short: "Resolve an email or pass through an external principal ID", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		if strings.TrimSpace(kind) == "" {
			return fmt.Errorf("--kind is required")
		}
		if (strings.TrimSpace(email) == "") == (strings.TrimSpace(externalID) == "") {
			return fmt.Errorf("exactly one of --email or --external-id is required")
		}
		c, err := e.newClient()
		if err != nil {
			return err
		}
		out, raw, err := c.ResolvePrincipal(e.ctx(), e.scopeProject(), client.PrincipalResolveRequest{
			Kind: kind, Email: email, ExternalID: externalID, Tenant: e.scopeTenant(),
		})
		if err != nil {
			return err
		}
		if e.jsonOut() {
			return e.renderJSONInline(raw)
		}
		_, _ = fmt.Fprintln(e.out, out.ExternalID)
		return nil
	}}
	resolve.Flags().StringVar(&kind, "kind", "", "declared principal kind")
	resolve.Flags().StringVar(&email, "email", "", "authenticated user's email")
	resolve.Flags().StringVar(&externalID, "external-id", "", "already-canonical principal external ID")
	cmd.AddCommand(resolve)
	return cmd
}

func readCLIFile(stdin io.Reader, path string) ([]byte, error) {
	if path == "-" {
		if stdin == nil {
			stdin = os.Stdin
		}
		return io.ReadAll(stdin)
	}
	return os.ReadFile(path)
}

func jwtStringClaim(token, key string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}
	var claims map[string]any
	if json.Unmarshal(raw, &claims) != nil {
		return ""
	}
	v, _ := claims[key].(string)
	return v
}

func randomMessageID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

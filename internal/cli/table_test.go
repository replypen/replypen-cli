package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestTableGolden is the one body every plain golden-table case shared: a stub server, a run, and a
// byte-for-byte golden. Members live here only when their body is exactly that — anything with extra
// setup (env vars, a pinned clock, a custom stub) or extra asserts keeps its own test.
func TestTableGolden(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		golden string
	}{
		{"AskEmailTableGolden", []string{"ask", "email-rich"}, "ask_email.golden"},
		{"AskRawTableGolden", []string{"ask", "show billing counts", "--scenario", "raw"}, "ask_raw.golden"},
		// AskDryScopeTableGolden: --dry-scope resolves the principal scope and renders the record without
		// running the agent. This dashboard_member self-assertion surfaces as tenant_wide — the finding target.
		{"AskDryScopeTableGolden", []string{"ask", "widen me", "--dry-scope", "--principal-kind", "dashboard_member", "--principal-id", "usr_42", "--asserted-by", "rootcause_session"}, "ask_dryscope.golden"},
		// AskDryScopeRefusedTableGolden: a fail-closed refusal prints the reason and no record.
		{"AskDryScopeRefusedTableGolden", []string{"ask", "refuse-me", "--dry-scope", "--principal-kind", "probackup_user", "--principal-id", "usr_1"}, "ask_dryscope_refused.golden"},
		{"RepoListTable", []string{"project", "repo", "ls"}, "repo_ls.golden"},
		{"RepoAddTable", []string{"project", "repo", "add", "id=momentum-web", "git_url=https://github.com/acme/momentum-web.git"}, "repo_add.golden"},
		{"RepoSetTable", []string{"project", "repo", "set", "momentum-web", "description=Updated"}, "repo_set.golden"},
		{"TenantListTable", []string{"project", "tenant", "ls"}, "tenant_ls.golden"},
		{"TenantAddTable", []string{"project", "tenant", "add", "slug=acme", "name=Acme Dental"}, "tenant_add.golden"},
		{"TenantGetTable", []string{"project", "tenant", "get", "acme"}, "tenant_add.golden"},
		{"ConnectionListTable", []string{"project", "connection", "ls"}, "connection_ls.golden"},
		{"ConnectionAddTable", []string{"project", "connection", "add", "name=podio", "kind=api_key"}, "connection_add.golden"},
		{"ConnectionProbeTable", []string{"project", "connection", "probe", "notion.write", "--write", "--notion-page", "page-123", "--cleanup"}, "connection_probe.golden"},
		{"ConnectionRotateTable", []string{"project", "connection", "rotate", "11111111-1111-1111-1111-111111111111"}, "connection_rotate.golden"},
		{"MemberListTable", []string{"project", "member", "ls"}, "member_ls.golden"},
		{"MemberAddTable", []string{"project", "member", "add", "id=carol@acme.test", "role=editor"}, "member_add.golden"},
		{"MailboxWatchedListTable", []string{"project", "mailbox", "ls"}, "mailbox_watched_ls.golden"},
		{"MailboxModeTable", []string{"--project", "alpha", "project", "mailbox", "mode", "11111111-1111-1111-1111-111111111111", "watch"}, "mailbox_mode.golden"},
		{"DatabaseListTable", []string{"project", "database", "ls"}, "database_ls.golden"},
		{"DatabaseGetTable", []string{"project", "database", "get", "primary"}, "database_get.golden"},
		{"DatabaseSetTable", []string{"project", "database", "set", "primary", "description=Primary OLTP"}, "database_get.golden"},
		{"GitHubStatusTable", []string{"project", "github", "status"}, "github_status.golden"},
		{"BrainStatusTable", []string{"dev", "brain", "status"}, "brain_status.golden"},
		{"BrainSyncTable", []string{"dev", "brain", "sync"}, "brain_sync.golden"},
		{"BrainPromoteTable", []string{"dev", "brain", "promote", "--channel", "stable", "--sha", "D2F9DE784AB7CDED001F2B6AC86892795F58A8CE"}, "brain_promote.golden"},
		{"BrainRenderTable", []string{"--project", "alpha", "--tenant", "de-linde", "dev", "brain", "render"}, "brain_render.golden"},
		{"MirrorRefreshTable", []string{"dev", "mirror", "refresh", "--repo", "kampadmin-replypen-common", "--expect-sha", "D2F9DE784AB7CDED001F2B6AC86892795F58A8CE"}, "mirror_refresh.golden"},
		{"MirrorRefreshTenantTable", []string{"--project", "alpha", "--tenant", "demo", "dev", "mirror", "refresh", "--repo", "site", "--expect-sha", "d2f9de784ab7cded001f2b6ac86892795f58a8ce"}, "mirror_refresh_tenant.golden"},
		{"BrainEditTable", []string{"dev", "brain", "edit", "add", "a", "runbook", "for", "refunds"}, "brain_edit.golden"},
		{"BrainConsolidateTable", []string{"dev", "brain", "consolidate"}, "brain_consolidate.golden"},
		{"BrainDeveloperInviteTable", []string{"--project", "alpha", "--tenant", "evident", "dev", "brain", "developer", "invite", "ardeae-praktijk"}, "brain_developer_invitation.golden"},
		{"AdminUserListTable", []string{"admin", "user", "ls"}, "admin_user_ls.golden"},
		{"AdminUserAddTable", []string{"admin", "user", "add", "email=dana@acme.test", "admin=true"}, "admin_user_add.golden"},
		{"AdminProjectListTable", []string{"admin", "project", "ls"}, "admin_project_ls.golden"},
		{"IdentityFailuresTable", []string{"admin", "project", "identity-failures", "alpha"}, "identity_failures.golden"},
		{"AdminCatalogUpsertTable", []string{"admin", "catalog", "upsert", "key=podio", "kind=api_key"}, "admin_catalog_upsert.golden"},
		// DeployStateTable pins the three-plane render: host release + brain channels/promotions +
		// mirrors with the refresh timeline (fixture uses canned timestamps).
		{"DeployStateTable", []string{"fleet", "deploy-state"}, "deploy_state.golden"},
		{"ExportListTable", []string{"project", "corpus", "ls"}, "export_ls.golden"},
		{"ExportGetTable", []string{"project", "corpus", "get", "eeee1111-0000-0000-0000-000000000001"}, "export_get.golden"},
		{"StatusTable", []string{"status"}, "status.golden"},
		{"RunsTable", []string{"run", "list", "--limit", "10"}, "runs.golden"},
		{"RunFeedbackListTable", []string{"run", "feedback", "list"}, "feedback_list.golden"},
		{"RunDetailTable", []string{"run", "show", "11111111-1111-1111-1111-111111111111"}, "run.golden"},
		{"RunEventsTable", []string{"run", "events", "11111111-1111-1111-1111-111111111111"}, "events.golden"},
		{"RunFullTable", []string{"run", "trace", "11111111-1111-1111-1111-111111111111"}, "full.golden"},
		// RunDeclinedTable pins the index "why" one-liner for a run that declined (the motivating case:
		// the CLI previously showed `declined` with no WHY). It must surface the truncated decline_reason plus
		// the guardrail/forced/fallback flags on a single Why: row.
		{"RunDeclinedTable", []string{"run", "show", "declined"}, "run_declined.golden"},
		// RunDeclinedEventsTable pins the trace's terminal-decline rendering: the reply event shows the
		// decline_reason instead of a draft/note line.
		{"RunDeclinedEventsTable", []string{"run", "events", "declined"}, "events_declined.golden"},
		// RunDeclinedFullTable pins the full header's debug rows + the untruncated decline_reason block.
		{"RunDeclinedFullTable", []string{"run", "trace", "declined"}, "full_declined.golden"},
		// RunGuardsTable pins the security-checkpoint readback: one row per checkpoint in evaluation
		// order, with blocks/violations/fail-open surfaced loudly.
		{"RunGuardsTable", []string{"run", "guards", "guarded"}, "guards.golden"},
		// RunBrainDiffTable pins the brain-diff renderer: the commit header (short sha, author, time,
		// subject), the touched files with churn, and the unified diff.
		{"RunBrainDiffTable", []string{"run", "brain-diff", "11111111-1111-1111-1111-111111111111"}, "brain_diff.golden"},
		// RunBrainDiffNotFoundTable pins the explicit-empty case: a run that wrote no journal commit shows
		// the "No brain changes from this run." line, not an error.
		{"RunBrainDiffNotFoundTable", []string{"run", "brain-diff", "no-brain"}, "brain_diff_none.golden"},
		{"BashListTable", []string{"dev", "console", "bash", "list"}, "bash_list.golden"},
		// ThreadTraceTable pins the thread trace: how the id resolved, the newest-first runs table with
		// health flags + placement, and the "Likely:" hint on the newest (egress-blocked) run.
		{"ThreadTraceTable", []string{"run", "thread", "thread-abc123"}, "thread_trace.golden"},
		// ThreadTraceSessionTable pins the session-fallback path (resolved_by:"session") and the declined-run
		// hint (the agent's own words on a declined run).
		{"ThreadTraceSessionTable", []string{"run", "thread", "session-fallback"}, "thread_trace_session.golden"},
		// ThreadTraceProviderTable pins the pre-run diagnostic path: a provider conversation id resolves
		// to its local channel row and explains a triage skip even though no run exists.
		{"ThreadTraceProviderTable", []string{"run", "thread", "215475391714527"}, "thread_trace_provider.golden"},
		// ThreadTraceSecurityBlockTable pins the pre-agent injection block: the thread went terminal with no
		// run row, so the loud SECURITY-BLOCK line (stage/category) is the only place the verdict surfaces.
		{"ThreadTraceSecurityBlockTable", []string{"run", "thread", "blocked-thread-xyz"}, "thread_trace_blocked.golden"},
		// ThreadTranscriptTable pins the conversation view: turns in created_at order (the thread trace
		// ships them newest-first), the `User selected:` turn folded under the question it answers, and the
		// footer counts that keep it honest.
		{"ThreadTranscriptTable", []string{"run", "thread", "chat-session", "--transcript"}, "thread_transcript.golden"},
		// RunTraceBriefTable pins the reading view of ONE turn: no system prompt, no tenant-settings blobs,
		// no timeline — but the drift those blobs imply survives as a count.
		{"RunTraceBriefTable", []string{"run", "trace", "chat-turn-1", "--brief"}, "trace_brief.golden"},
		// ThreadTraceUnknownTable pins the explicit-empty case: an unknown id is a clean "no runs" answer
		// (resolved_by:"none"), not an error.
		{"DraftRenderTable", []string{"--project", "alpha", "dev", "tools", "draft-render", "testdata/draft_render.md"}, "draft_render.golden"},
		{"ThreadTraceUnknownTable", []string{"run", "thread", "unknown"}, "thread_trace_none.golden"},
		// ThreadTraceGmailLinkTable: a pasted Gmail print link resolves to its hex thread id; with no record,
		// the Google mailboxes (never the same-address Microsoft/Intercom rows) and the likely ingestion gap print.
		{"ThreadTraceGmailLinkTable", []string{"--project", "alpha", "run", "thread", "https://mail.google.com/mail/u/1/?ik=85be6e80b2&view=pt&search=all&permthid=thread-f:1879575123597418725&simpl=msg-f:1879575123597418725"}, "thread_trace_gmail_none.golden"},
		{"SettingProposalsTable", []string{"--project", "alpha", "project", "settings", "proposals", "ls"}, "setting_proposals_ls.golden"},
		{"ReviewListTable", []string{"--project", "alpha", "project", "review", "ls", "--audience", "technical"}, "review_ls.golden"},
		// show/close without --audience: owner 404s, the technical retry answers.
		{"ReviewShowTable", []string{"--project", "alpha", "project", "review", "show", "75d0a3dd-d39c-40a2-af97-ae46fed56a3d"}, "review_show.golden"},
		{"ReviewCloseTable", []string{"--project", "alpha", "project", "review", "close", "75d0a3dd-d39c-40a2-af97-ae46fed56a3d", "--note", "fixed in abc123"}, "review_close.golden"},
		{"ReviewDecideTable", []string{"--project", "alpha", "project", "review", "decide", "75d0a3dd-d39c-40a2-af97-ae46fed56a3d", "--label", "Do it", "--audience", "technical"}, "review_decide.golden"},
		// A UUID --project is mapped to its name: the review routes resolve {project} by name only.
		{"ReviewListByProjectIDTable", []string{"--project", "aaaaaaaa-0000-0000-0000-000000000001", "project", "review", "ls", "--audience", "technical"}, "review_ls.golden"},
		{"ReviewFinishTable", []string{"--project", "alpha", "project", "review", "finish", "--audience", "technical"}, "review_finish.golden"},
		{"ConfigGetTable", []string{"project", "settings", "runtime", "get"}, "config_get.golden"},
		{"ConfigSetTable", []string{"project", "settings", "runtime", "set", `models.agent={"tier":"pro"}`, "max_run_usd=5"}, "config_set.golden"},
		// ConfigSetListValue locks the list-coercion contract: `project settings runtime set pr.triggers=inbound,mcp` sends a
		// JSON ARRAY (asserted in the PATCH handler), not a comma string. The handler fatals if it's not an array.
		{"ConfigSetListValue", []string{"project", "settings", "runtime", "set", "pr.triggers=inbound,mcp"}, "config_set.golden"},
		// KBGetTable pins `rp project knowledge sync get` — the generic bag command over a non-settings
		// bag renders the same {key:value/effective/default/source} table as project runtime settings.
		{"KBGetTable", []string{"project", "knowledge", "sync", "get"}, "kb_get.golden"},
		// KBSetTable pins `rp project knowledge sync set provider=intercom` round-tripping through PATCH /api/v1/kb.
		{"KBSetTable", []string{"project", "knowledge", "sync", "set", "provider=intercom", "base_url=https://acme.intercom.io"}, "kb_get.golden"},
		// The help-centre write path. Dry-run is the review surface: the verdict line plus the FULL
		// provider calls that would be sent. The real write is one terse line, and an unchanged block
		// is an explicit no-op rather than a wasted provider call.
		{"KnowledgeArticleApplyDryTable", []string{"project", "knowledge", "article", "apply", "--from", "testdata/knowledge_article_block.md", "--dry-run"}, "knowledge_article_apply_dry.golden"},
		{"KnowledgeArticleApplyTable", []string{"project", "knowledge", "article", "apply", "--from", "testdata/knowledge_article_block.md", "--publish"}, "knowledge_article_apply.golden"},
		{"KnowledgeArticleApplyNoChangeTable", []string{"project", "knowledge", "article", "apply", "--from", "testdata/knowledge_article_block_nochange.md"}, "knowledge_article_apply_nochange.golden"},
		// get prints the block verbatim — it is the input of the next apply, so nothing may be reformatted.
		{"KnowledgeArticleGetTable", []string{"project", "knowledge", "article", "get", "--provider", "helpscout", "--id", "5f1a2b3c4d5e6f7a8b9c0d1e"}, "knowledge_article_get.golden"},
		{"SchemaTable", []string{"project", "settings", "schema"}, "schema.golden"},
		{"ExplainTable", []string{"project", "settings", "describe", "models.agent"}, "explain_models_agent.golden"},
		{"AccessTable", []string{"auth", "access"}, "access.golden"},
		// SpamListTable pins `rp project senders ls`: both lists rendered as one VERDICT/PATTERN/TYPE/SOURCE/CREATED
		// table (allows first, then blocks), in server order.
		{"SpamListTable", []string{"project", "senders", "ls"}, "spam_ls.golden"},
		// SpamAllowTable pins `rp project senders allow <pattern> --reason …`: the echoed rule with the
		// server-inferred match_type renders as a one-row table.
		{"SpamAllowTable", []string{"project", "senders", "allow", "@partner.example", "--reason", "trusted"}, "spam_allow.golden"},
		// SpamAllowMailboxTable pins `rp project senders allow <pattern> --mailbox <uuid>`: the mailbox_id rides in the
		// POST body (asserted server-side by echoing it back as "mailbox"), and the echoed rule renders with the
		// MAILBOX column populated.
		{"SpamAllowMailboxTable", []string{"project", "senders", "allow", "@partner.example", "--mailbox", "mbx11111-0000-0000-0000-000000000009"}, "spam_allow_mailbox.golden"},
		// SpamListMailboxFilter pins `rp project senders ls --mailbox <uuid>`: the client-side filter narrows the table
		// to the two rules (one allow, one block) scoped to that mailbox, dropping the project-scoped rows.
		{"SpamListMailboxFilter", []string{"project", "senders", "ls", "--mailbox", "mbx11111-0000-0000-0000-000000000001"}, "spam_ls_mailbox.golden"},
		// SpamBlockTable pins `rp project senders block <pattern>` (no reason).
		{"SpamBlockTable", []string{"project", "senders", "block", "junk@spammy.example"}, "spam_block.golden"},
		// The guarded console read/exec views (`rp dev console …`) had no golden at all — the renderers were
		// dead to the test suite. One case each, over fixtures that exercise the optional blocks: capabilities
		// with every plane populated, an action manifest with autonomy floors / connections / params / stats,
		// and both arms of the exec view (a result body vs an error body).
		{"ConsoleCapabilitiesTable", []string{"dev", "console", "capabilities"}, "console_capabilities.golden"},
		{"ConsoleDBListTable", []string{"dev", "console", "database", "list"}, "console_db_list.golden"},
		{"ConsoleDBSchemaTable", []string{"dev", "console", "database", "schema", "app"}, "console_db_schema.golden"},
		{"ConsoleActionListTable", []string{"dev", "console", "action", "list"}, "console_action_list.golden"},
		{"ConsoleActionShowTable", []string{"dev", "console", "action", "show", "cancel_subscription"}, "console_action_show.golden"},
		// FleetTable pins the fleet digest: the per-run flag line (incl. the client-computed T! turn
		// spike), the aggregate, and the worst-offender shortlists. The --kind fleet param routes the stub to
		// the operator-tier (health-bearing) paged fixtures.
		{"FleetTable", []string{"fleet", "runs", "--kind", "fleet"}, "fleet.golden"},
		// FleetTimeline pins the per-day runs/errors/latency histogram (the "what changed today" anchor).
		{"FleetTimeline", []string{"fleet", "runs", "--kind", "fleet", "--timeline"}, "fleet_timeline.golden"},
		// PatternsTable pins the run_patterns port: the bash-failure clusters (twin orders_2024/2025 collapse
		// to one signature across 2 runs via masking) + the blocked-egress host cluster, each with a suggested-fix
		// stub.
		{"PatternsTable", []string{"fleet", "patterns"}, "patterns.golden"},
		// HealthCleanExitsZero: a clean fleet renders healthy AND returns nil (zero exit).
		{"HealthCleanExitsZero", []string{"fleet", "health", "--hours", "999"}, "health_clean.golden"},
		{"TenantSettingsGetTable", []string{"project", "tenant", "settings", "get", "de-kies"}, "tenant_get.golden"},
		// RepliesListTable pins `rp dev replies list`: one line per turn, the decomposed send state, and the
		// next-page cursor hint.
		{"RepliesListTable", []string{"--project", "alpha", "--tenant", "de-kies", "dev", "replies", "list", "--limit", "2"}, "replies_ls.golden"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := stubServer(t)
			defer srv.Close()
			e, out, _ := newTestEnv(t, srv, "table")
			if err := run(t, e, tc.args...); err != nil {
				t.Fatalf("rp %s: %v", strings.Join(tc.args, " "), err)
			}
			// The stub's port changes per run; links printed against it are pinned as <base>.
			assertGolden(t, tc.golden, strings.ReplaceAll(out.String(), srv.URL, "<base>"))
		})
	}
}

// TestJSONPassthrough pins the `-o json` contract for every command whose JSON test was exactly
// "run it, compare the body to the fixture": the server's body must round-trip unreshaped.
func TestJSONPassthrough(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		fixture string
	}{
		{"RepoListJSONPassthrough", []string{"project", "repo", "ls"}, "repos.json"},
		{"IdentityFailuresJSONPassthrough", []string{"admin", "project", "identity-failures", "alpha"}, "identity_failures.json"},
		{"TenantGetJSONPassthrough", []string{"project", "tenant", "get", "acme"}, "tenant_item.json"},
		{"ConnectionListJSONPassthrough", []string{"project", "connection", "ls"}, "connections.json"},
		{"ConnectionProbeJSONPassthrough", []string{"project", "connection", "probe", "notion.write"}, "connection_probe.json"},
		{"MemberListJSONPassthrough", []string{"project", "member", "ls"}, "members.json"},
		{"TokenListJSONPassthrough", []string{"project", "token", "ls", "--include-revoked"}, "tokens.json"},
		{"MailboxWatchedListJSONPassthrough", []string{"project", "mailbox", "ls"}, "watched_mailboxes.json"},
		{"DatabaseControlsGetJSON", []string{"project", "database", "controls", "get", "primary"}, "database_controls.json"},
		{"GitHubStatusJSONPassthrough", []string{"project", "github", "status"}, "github_status.json"},
		{"BrainStatusJSONPassthrough", []string{"dev", "brain", "status"}, "brain_status.json"},
		{"BrainPromoteJSONPassthrough", []string{"--project", "alpha", "dev", "brain", "promote", "--channel", "stable", "--sha", "d2f9de784ab7cded001f2b6ac86892795f58a8ce"}, "brain_promote.json"},
		{"BrainRenderJSONPassthrough", []string{"--project", "alpha", "--tenant", "de-linde", "dev", "brain", "render"}, "brain_render.json"},
		{"MirrorRefreshJSONPassthrough", []string{"--project", "alpha", "dev", "mirror", "refresh", "--repo", "kampadmin-replypen-common", "--expect-sha", "d2f9de784ab7cded001f2b6ac86892795f58a8ce"}, "mirror_refresh.json"},
		{"MirrorRefreshTenantJSONPassthrough", []string{"--project", "alpha", "--tenant", "demo", "dev", "mirror", "refresh", "--repo", "site", "--expect-sha", "d2f9de784ab7cded001f2b6ac86892795f58a8ce"}, "mirror_refresh_tenant.json"},
		{"BrainDeveloperInviteJSONPassthrough", []string{"--project", "alpha", "--tenant", "evident", "dev", "brain", "developer", "invite", "ardeae-praktijk"}, "brain_developer_invitation.json"},
		// DeployStateJSONPassthrough: without --host-repo the -o json body is the verbatim server rows.
		{"DeployStateJSONPassthrough", []string{"fleet", "deploy-state"}, "deploy_state.json"},
		{"ExportListJSONPassthrough", []string{"project", "corpus", "ls"}, "exports.json"},
		{"ExportGetJSONPassthrough", []string{"project", "corpus", "get", "eeee1111-0000-0000-0000-000000000001"}, "export_item.json"},
		// RunDeclinedJSONPassthrough confirms the new debug fields ride through -o json verbatim (the CLI
		// reshapes nothing): the raw server body round-trips unchanged.
		{"RunDeclinedJSONPassthrough", []string{"run", "show", "declined"}, "run_declined.json"},
		// RunBrainDiffJSONPassthrough confirms -o json rides the server body through verbatim (the CLI
		// reshapes nothing).
		{"RunBrainDiffJSONPassthrough", []string{"run", "brain-diff", "11111111-1111-1111-1111-111111111111"}, "brain_diff.json"},
		// ThreadTraceJSONPassthrough confirms -o json emits the server body verbatim — the CLI reshapes
		// nothing.
		{"ThreadTraceJSONPassthrough", []string{"run", "thread", "thread-abc123"}, "thread_trace.json"},
		{"KnowledgeArticleApplyJSONPassthrough", []string{"project", "knowledge", "article", "apply", "--from", "testdata/knowledge_article_block.md", "--dry-run"}, "knowledge_article_apply_dry.json"},
		{"KnowledgeSummariesApplyJSONPassthrough", []string{"project", "knowledge", "summaries", "apply", "--from", "testdata/knowledge_summaries_patches.json", "--dry-run"}, "knowledge_summaries_apply.json"},
		{"KnowledgeSummariesApplyTenantJSONPassthrough", []string{"--project", "alpha", "--tenant", "acme", "project", "knowledge", "summaries", "apply", "--from", "testdata/knowledge_summaries_patches.json", "--dry-run"}, "knowledge_summaries_apply.json"},
		{"KnowledgeArticleGetJSONPassthrough", []string{"project", "knowledge", "article", "get", "--provider", "helpscout", "--id", "5f1a2b3c4d5e6f7a8b9c0d1e"}, "knowledge_article_get.json"},
		{"SchemaJSONPassthrough", []string{"project", "settings", "schema"}, "meta_schema.json"},
		{"AccessJSONPassthrough", []string{"auth", "access"}, "meta_capabilities.json"},
		{"StatusJSONPassthrough", []string{"status"}, "runs.json"},
		{"RunDetailJSONPassthrough", []string{"run", "show", "11111111-1111-1111-1111-111111111111"}, "run.json"},
		{"RunFeedbackListJSONPassthrough", []string{"run", "feedback", "list"}, "feedback.json"},
		{"SettingProposalsJSONPassthrough", []string{"--project", "alpha", "--tenant", "acme", "project", "settings", "proposals", "ls"}, "setting_proposals.json"},
		{"ReviewListJSONPassthrough", []string{"--project", "alpha", "--tenant", "acme", "project", "review", "ls", "--audience", "technical"}, "review.json"},
		{"ConfigGetJSONPassthrough", []string{"project", "settings", "runtime", "get"}, "settings.json"},
		{"TenantSettingsGetJSON", []string{"project", "tenant", "settings", "get", "de-kies"}, "hierarchy_tenant_settings.json"},
		{"TenantProfileGetJSON", []string{"project", "tenant", "profile", "get", "de-kies"}, "tenant_settings.json"},
		{"TenantProfileSchemaDump", []string{"project", "tenant", "profile", "schema"}, "tenant_schema.json"},
		{"ChatCardDecideEmbedJSON", []string{"--project", "alpha", "project", "chat", "decide", "sess_1", "wc_01", "publish", "--token", "test-key", "--origin", "https://site.example.test"}, "chat_card.json"},
		{"ChatCardStatusEmbedJSON", []string{"--project", "alpha", "project", "chat", "card", "sess_1", "wc_01", "--token", "test-key", "--origin", "https://site.example.test"}, "chat_card.json"},
		{"ChatCardDecideDashboardJSON", []string{"--project", "alpha", "project", "chat", "decide", "sess_1", "wc_01", "publish", "--lane", "setup"}, "chat_card.json"},
		{"ChatCardStatusDashboardJSON", []string{"--project", "alpha", "project", "chat", "card", "sess_1", "wc_01", "--lane", "setup"}, "chat_card.json"},
		{"ProjectHierarchySettingsGetJSON", []string{"project", "settings", "behavior", "get"}, "hierarchy_project_settings.json"},
		{"RepliesListJSONPassthrough", []string{"--project", "alpha", "--tenant", "de-kies", "dev", "replies", "list"}, "replies.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := stubServer(t)
			defer srv.Close()
			e, out, _ := newTestEnv(t, srv, "json")
			if err := run(t, e, tc.args...); err != nil {
				t.Fatalf("rp %s: %v", strings.Join(tc.args, " "), err)
			}
			assertJSONEqual(t, fixture(t, tc.fixture), out.Bytes())
		})
	}
}

// TestTableLine covers the commands whose whole table output is one acknowledgement line, asserted
// exactly (the ack wording is the contract for a scripted caller reading stdout).
func TestTableLine(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"RepoRmTable", []string{"project", "repo", "rm", "momentum-web"}, "deleted repos momentum-web\n"},
		{"ConnectionRmTable", []string{"project", "connection", "rm", "11111111-1111-1111-1111-111111111111"}, "revoked and deleted connection 11111111-1111-1111-1111-111111111111\n"},
		{"MemberRmTable", []string{"project", "member", "rm", "bob@acme.test"}, "deleted members bob@acme.test\n"},
		{"TokenRevokeTable", []string{"project", "token", "revoke", "tok_aaaa"}, "revoked token tok_aaaa\n"},
		{"EnvRmTable", []string{"project", "env", "rm", "STRIPE_KEY"}, "deleted STRIPE_KEY (env_grounding)\n"},
		{"BrandingLogoClear", []string{"project", "branding", "logo", "clear"}, "logo cleared\n"},
		{"RepliesFeedbackTable", []string{"--project", "alpha", "--tenant", "de-kies", "dev", "replies", "feedback", "11111111-1111-1111-1111-111111111111", "--score", "5", "--comment", "te formeel"}, "Scope: alpha / de-kies\nfeedback recorded for run 11111111-1111-1111-1111-111111111111\n"},
		{"RunFeedbackTable", []string{"run", "feedback", "11111111-1111-1111-1111-111111111111", "--score", "1", "--comment", "great draft"}, "feedback recorded for run 11111111-1111-1111-1111-111111111111\n"},
		// RunFeedbackProcessedTable: the operator plane rides a PATCH and prints its own terse line.
		{"RunFeedbackProcessedTable", []string{"run", "feedback", "11111111-1111-1111-1111-111111111111", "--processed", "--resolution-note", "fixed the brain skill"}, "feedback marked processed with a resolution note for run 11111111-1111-1111-1111-111111111111\n"},
		// RunRetryPrintsNewID: retry prints the NEW run id on stdout (the table path), capturable for chaining.
		{"RunRetryPrintsNewID", []string{"run", "retry", "11111111-1111-1111-1111-111111111111", "--tier", "pro"}, "99999999-9999-9999-9999-999999999999\n"},
		// RunRetryWithComment: reviewer steering rides the retry POST as "comment"; no --tier, so the
		// server keeps the original tier.
		{"RunRetryWithComment", []string{"run", "retry", "11111111-1111-1111-1111-111111111111", "--comment", "  the correct answer is 42\n"}, "99999999-9999-9999-9999-999999999999\n"},
		{"SettingProposalApprove", []string{"--project", "alpha", "project", "settings", "proposals", "approve", "5A1F0C2E-1111-4000-8000-000000000001"}, "applied settings proposal 5a1f0c2e-1111-4000-8000-000000000001: persona.tone = warm\n"},
		{"SettingProposalReject", []string{"--project", "alpha", "--tenant", "acme", "project", "settings", "proposals", "reject", "5a1f0c2e-2222-4000-8000-000000000002", "--reason", "too broad"}, "Scope: alpha / acme\nrejected settings proposal 5a1f0c2e-2222-4000-8000-000000000002: persona.tone = warm\n"},
		{"RunProcessThreadPrintsStatusURL", []string{"run", "process-thread", "thread-1"}, "/api/v1/projects/alpha/inbox/threads/thread-1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := stubServer(t)
			defer srv.Close()
			e, out, _ := newTestEnv(t, srv, "table")
			if err := run(t, e, tc.args...); err != nil {
				t.Fatalf("rp %s: %v", strings.Join(tc.args, " "), err)
			}
			if got := out.String(); got != tc.want {
				t.Errorf("rp %s output = %q, want %q", strings.Join(tc.args, " "), got, tc.want)
			}
		})
	}
}

// TestRejectsBeforeRequest collects the client-side validations: a bad flag combination or a bad
// value must fail with its own message rather than reach the server (the stub would happily answer).
// Each row asserts the command's own request was never sent; only the scope probes (scopeProbes) may run.
func TestRejectsBeforeRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		// --sha and --channel name two different commits; accepting both would silently pick one.
		{"BrainRenderRejectsShaAndChannelTogether", []string{"--project", "alpha", "--tenant", "de-linde", "dev", "brain", "render", "--sha", "d2f9de784ab7cded001f2b6ac86892795f58a8ce", "--channel", "stable"}, "mutually exclusive"},
		// preflight is project-only: an ambient --tenant would silently narrow a fleet-wide question.
		{"BrainPreflightRejectsTenantSelector", []string{"--tenant", "de-kies", "dev", "brain", "preflight", "--sha", "d2f9de784ab7cded001f2b6ac86892795f58a8ce"}, "--tenant is not supported"},
		// RunFeedbackProcessedExclusive: the two processed flags contradict each other; reject client-side.
		{"RunFeedbackProcessedExclusive", []string{"run", "feedback", "11111111-1111-1111-1111-111111111111", "--processed", "--unprocessed"}, "processed"},
		// RunFeedbackRequiresInput: with no flag at all it's a clear client-side error.
		{"RepliesRequireTenant", []string{"--project", "alpha", "dev", "replies", "list"}, "--tenant are required"},
		{"RepliesFeedbackRequiresInput", []string{"--project", "alpha", "--tenant", "de-kies", "dev", "replies", "feedback", "11111111-1111-1111-1111-111111111111"}, "nothing to record"},
		{"RunFeedbackRequiresInput", []string{"run", "feedback", "11111111-1111-1111-1111-111111111111"}, "nothing to record"},
		{"RunFeedbackListRejectsScore", []string{"run", "feedback", "list", "--score", "1"}, "invalid --score"},
		{"RunFeedbackListRejectsSurface", []string{"run", "feedback", "list", "--surface", "mobile"}, "invalid --surface"},
		{"KBSearchRejectsProviderTraversal", []string{"project", "knowledge", "content", "search", "--provider", "../agent_internal", "restore"}, "invalid --provider"},
		// apply without a block has nothing to send; naming --from beats a server 400 on an empty body.
		{"KnowledgeArticleApplyRequiresFrom", []string{"project", "knowledge", "article", "apply"}, "--from"},
		{"KnowledgeSummariesApplyRequiresFrom", []string{"project", "knowledge", "summaries", "apply"}, "--from"},
		// The get provider is a PATH segment: an unknown/traversing value never leaves the client.
		{"KnowledgeArticleGetRejectsProvider", []string{"project", "knowledge", "article", "get", "--provider", "../agent_internal", "--id", "19"}, "invalid --provider"},
		{"TenantSettingsGetMissingTenant", []string{"project", "tenant", "settings", "get"}, "arg"},
		{"IdentityFailuresMissingProject", []string{"admin", "project", "identity-failures"}, "arg"},
		{"IdentityFailuresRejectsProjectFlag", []string{"admin", "project", "identity-failures", "alpha", "--project", "beta"}, "--project is not supported"},
		{"IdentityFailuresRejectsTenant", []string{"admin", "project", "identity-failures", "alpha", "--tenant", "north"}, "--tenant is not supported"},
		// The console principal pair: half a principal is a scoping mistake (silent under- or over-scope),
		// so each console verb refuses before the request instead of binding a partial identity.
		// The steering text has one source; accepting both would silently pick one.
		{"RunRetryRejectsCommentAndCommentFile", []string{"run", "retry", "11111111-1111-1111-1111-111111111111", "--comment", "hi", "--comment-file", "-"}, "none of the others can be"},
		// Whitespace-only steering would reach the server as an empty comment; refuse instead.
		// Close without a note would retire a card with no trace of where it was handled.
		{"ReviewCloseRequiresNote", []string{"--project", "alpha", "project", "review", "close", "75d0a3dd-d39c-40a2-af97-ae46fed56a3d"}, "--note is required"},
		{"ReviewDecideRequiresLabel", []string{"--project", "alpha", "project", "review", "decide", "75d0a3dd-d39c-40a2-af97-ae46fed56a3d"}, "--label is required"},
		{"ReviewRejectsPrefixID", []string{"--project", "alpha", "project", "review", "close", "75d0a3dd", "--note", "x"}, "full card id"},
		{"ReviewRejectsAudience", []string{"--project", "alpha", "project", "review", "ls", "--audience", "staff"}, "invalid --audience"},
		{"RunRetryRejectsEmptyComment", []string{"run", "retry", "11111111-1111-1111-1111-111111111111", "--comment", "   "}, "empty retry comment"},
		// The list is windowed (pending + recent decided), so a prefix could resolve to the wrong row:
		// decisions take the full uuid and never guess.
		{"SettingProposalApproveRejectsPrefix", []string{"--project", "alpha", "project", "settings", "proposals", "approve", "5a1f0c2e-1111"}, "full proposal id"},
		{"SettingProposalRejectRejectsPrefix", []string{"--project", "alpha", "project", "settings", "proposals", "reject", "5a1f0c2e"}, "full proposal id"},
		// --mirror-ref is <repo>=<ref>, one ref per repo: a malformed or duplicate pair never reaches the server.
		{"AskRejectsMirrorRefWithoutRef", []string{"ask", "q", "--mirror-ref", "app="}, "want <repo>=<ref>"},
		{"AskRejectsMirrorRefWithoutEquals", []string{"ask", "q", "--mirror-ref", "dev/x"}, "want <repo>=<ref>"},
		{"AskRejectsDuplicateMirrorRef", []string{"ask", "q", "--mirror-ref", "app=dev/a", "--mirror-ref", "app=dev/b"}, "more than once"},
		{"ConsoleBashRunRejectsHalfPrincipal", []string{"dev", "console", "bash", "run", "--principal-kind", "kampadmin_admin", "echo hi"}, "must be provided together"},
		{"ConsoleDBQueryRejectsHalfPrincipal", []string{"dev", "console", "database", "query", "prod", "select 1", "--principal-id", "usr_1"}, "must be provided together"},
		// paths are absolute literals checked in a production workspace: a relative path or an ambiguous
		// tenant selection never reaches the server.
		{"ConsolePathsRejectsRelativePath", []string{"dev", "console", "paths", "/brain/AGENTS.md", "tenant/AGENTS.md"}, "not absolute"},
		{"ConsolePathsRejectsTenantWithTenants", []string{"--tenant", "acme", "dev", "console", "paths", "--tenants", "beta", "/brain"}, "cannot be combined"},
		{"ConsoleDBSchemaRejectsHalfPrincipal", []string{"dev", "console", "database", "schema", "app", "--principal-kind", "kampadmin_admin"}, "must be provided together"},
		// session search narrows locally: a malformed filter never starts the paged scan.
		{"SessionSearchRejectsHalfPrincipalID", []string{"run", "sessions", "search", "--principal-id", "1"}, "must be provided together"},
		{"SessionSearchRejectsHalfPrincipalKind", []string{"run", "sessions", "search", "--principal-kind", "member"}, "must be provided together"},
		{"SessionSearchRejectsBadSince", []string{"run", "sessions", "search", "--since", "yesterday"}, "invalid --since"},
		{"SessionSearchRejectsBadUntil", []string{"run", "sessions", "search", "--until", "2026-02-30"}, "invalid --until"},
		{"SessionSearchRejectsInvertedWindow", []string{"run", "sessions", "search", "--since", "2026-02-02", "--until", "2026-02-01"}, "--since must precede --until"},
		{"SessionSearchRejectsZeroLimit", []string{"run", "sessions", "search", "--limit", "0"}, "--limit must be between"},
		{"SessionSearchRejectsBlankContains", []string{"run", "sessions", "search", "--contains", "  "}, "--contains must not be empty"},
		{"SessionSearchRejectsBadSurface", []string{"run", "sessions", "search", "--surface", "bad"}, "invalid --surface"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := stubServer(t)
			defer base.Close()
			var mu sync.Mutex
			var sent []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if call := r.Method + " " + r.URL.Path; !scopeProbes[call] {
					mu.Lock()
					sent = append(sent, call)
					mu.Unlock()
				}
				base.Config.Handler.ServeHTTP(w, r)
			}))
			defer srv.Close()
			e, _, _ := newTestEnv(t, srv, "table")
			err := run(t, e, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("rp %s error = %v, want %q", strings.Join(tc.args, " "), err, tc.want)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(sent) != 0 {
				t.Fatalf("rp %s reached the server before rejecting: %v", strings.Join(tc.args, " "), sent)
			}
		})
	}
}

// scopeProbes are the identity/scope reads env resolution makes before any command body runs; they are
// not the command's own request, so TestRejectsBeforeRequest tolerates them.
var scopeProbes = map[string]bool{"GET /api/v1/whoami": true, "GET /api/v1/projects": true}

// TestRejectsWithServerData: validations whose verdict needs a server read (the settings schema, the
// proposal list) or a server answer (a recorded apply failure). They fail with their own message, but
// by design only after a request.
func TestRejectsWithServerData(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		// ConfigSetObjectRejectsNonObject asserts a non-JSON-object value for an object key fails
		// client-side with a shape hint instead of being sent as a bare string.
		{"ConfigSetObjectRejectsNonObject", []string{"project", "settings", "runtime", "set", "models.agent=pro"}, "expected a JSON object"},
		// ExplainUnknownKey asserts an unknown key is a clear client-side error (not a silent miss).
		{"ExplainUnknownKey", []string{"project", "settings", "describe", "nope"}, "unknown config key"},
		{"SettingProposalAmbiguousPrefix", []string{"--project", "alpha", "project", "settings", "proposals", "show", "5a1f0c2e"}, "ambiguous"},
		// A recorded apply failure is HTTP 200 server-side; the CLI must still exit non-zero.
		{"SettingProposalApplyFailedExitsNonZero", []string{"--project", "alpha", "project", "settings", "proposals", "approve", "fa110000-0000-4000-8000-000000000000"}, "apply failed: settings changed since this preview"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := stubServer(t)
			defer srv.Close()
			e, _, _ := newTestEnv(t, srv, "table")
			err := run(t, e, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("rp %s error = %v, want %q", strings.Join(tc.args, " "), err, tc.want)
			}
		})
	}
}

# replypen-cli (`rp`)

A scriptable client that lets a project **consume its own ReplyPen data** and **change its own
config** — over ReplyPen's public JSON `/api/v1`, authed with an **OAuth access token** (you sign in
once with `rp auth login`; the CLI refreshes the token for you). **Fat client, thin server:** endpoints
return raw, token-scoped data; the CLI may digest/cluster/render it for you, and every such command also
emits the raw rows as **JSON when piped** so `| jq` always works and you can slice it your own way.

```console
$ rp status
Health: healthy

Sources:
  SOURCE      TOTAL  ERRORS
  Prompt API  12     0

$ rp ask "Do I still have open invoices?"   # simulate a support email, wait, print draft/note
$ rp ask --scenario raw "How many active subscriptions are past due?"
$ rp ask --effort pro "Retry this with a stronger model tier"
$ rp run list --kind prompt --limit 5 | jq '.runs[].run_id'
$ rp run list --outcome failed --learning             # learning candidates with failed verdicts
$ rp run list --from jara@example.be --since 2026-09-29  # forwarded email → its run (FROM/SUBJECT columns)
$ rp run thread jara@example.be                       # …or its threads when no run exists (triage skip)
$ rp run events <id>        # full per-iteration trace (NDJSON when piped)
$ rp run trace <id|url>      # GET /runs/{id}/trace bundle (header + trace; JSONL when piped)
$ rp fleet actions --days 14 --action create_appointment --action update_appointment
$ rp dev learning evidence --plane triage --limit 50 -o json
$ rp dev learning evidence --plane deltas --include-bodies -o json
$ rp dev learning evidence --plane shadow --days 14 --verdict divergent_facts,missed_content -o json
$ rp dev brain sync
$ rp dev brain promote --channel stable --sha "$(git rev-parse HEAD)"
$ rp dev brain status                 # verify stable/edge resolved SHAs before claiming success
$ rp project settings runtime set max_run_usd=5 'models.agent={"tier":"pro"}'
$ rp project settings behavior set persona.tone=warm channel.labeling_enabled=true
$ rp project chat set chat_hot_ttl_secs=null   # key=null resets a nullable knob to inherit
$ rp project triage policy get -o json
$ rp project triage rules ls -o json
$ rp project tenant settings get acme
$ rp project tenant profile get acme
$ rp project mailbox settings set <mailbox-id> persona.tone=direct
$ rp dev api routes | grep /trace
$ rp project env keys                  # key NAMES of the production grounding env (no values)
$ rp project env pull                  # sync that env to a local 0600 ./.env (for brain-dev --live)
```

## Install

You do **not** need Go installed — grab a prebuilt binary with the one-liner for your platform.

> [!NOTE]
> `rp` was called `rc` before the ReplyPen rename. Every installer (cask, scripts, `rp self update`) keeps
> `rc` as an alias of the same binary, so old commands and scripts keep working; an existing `rc` install
> upgrades in place. The token store moves from `~/.config/rootcause` to `~/.config/replypen` on first
> run (a symlink keeps older `rc` builds on the same tokens); `.rootcause.toml` markers,
> `.rootcause/local.toml` and `ROOTCAUSE_BASE_URL` are still read.

**macOS — Homebrew:**

```bash
brew install --cask replypen/tap/rp      # then: rp self update
```

> This is a **cask** (a prebuilt binary), not a source formula — it sidesteps the Homebrew sandbox/PTY
> install that fails on some macOS setups with `can't get Master/Slave device`. Quarantine is stripped
> automatically, so `rp` runs without a Gatekeeper prompt.
> Homebrew is the canonical macOS installation. Do not also install `rp` with Go or a standalone
> script; migrate an existing mixed setup with `rp self update --migrate`.

**Linux / WSL — install script** (no Homebrew or Go required):

```bash
curl -fsSL https://raw.githubusercontent.com/replypen/replypen-cli/main/scripts/install.sh | sh
```

Detects your arch, installs `rp` (plus the `rc` symlink) to `/usr/local/bin` (or `~/.local/bin`, or
wherever an existing `rp`/`rc` lives), and is idempotent — re-run to upgrade. Knobs: `RC_VERSION=v0.5.1` to pin, `RC_ASSET_SHA256=…` to pin the reviewed archive digest,
and `RC_INSTALL_DIR=…` to choose where. On macOS the script
uses Homebrew unless `RC_INSTALL_DIR` is explicitly set.

**Windows (native PowerShell):**

```powershell
irm https://raw.githubusercontent.com/replypen/replypen-cli/main/scripts/install.ps1 | iex
```

Installs `rp.exe` (plus an `rc.exe` copy) under `%LOCALAPPDATA%\Programs\rp`, retires a pre-rename
`%LOCALAPPDATA%\Programs\rc` install, and adds the new dir to your user PATH. (On **WSL**, use the
Linux one-liner above — WSL is Linux.)

**From source (Go developers, development builds only):**

```bash
go install github.com/replypen/replypen-cli/cmd/rp@latest
```

Tagged Go installs report the embedded module version correctly, but Homebrew remains the one supported
end-user installation on macOS.

**Manual** — grab a tarball/zip from the [latest release](https://github.com/replypen/replypen-cli/releases/latest)
(`darwin`/`linux`/`windows` × `amd64`/`arm64`), extract `rp`, and put it on your PATH. On macOS, an
unsigned binary may be quarantined: `xattr -d com.apple.quarantine $(which rp)`.

### Upgrading

```bash
rp self update            # self-update to the latest release (Linux / WSL / Windows)
rp self update --check    # just say whether a newer version exists
rp self doctor            # show executing binary, PATH selection, install kind, duplicates
rp self update --migrate  # macOS: canonicalize on Homebrew; remove verified legacy Go copies
```

`rp self update` replaces its own binary with the latest release for your OS/arch (verifying the published
checksum first). It normally reads GitHub Releases; set `RC_RELEASE_MIRROR=https://mirror.example/rp` to read a
trusted mirror whose `latest`, archive, and `checksums.txt` objects share the release layout. Release builds
also fall back to the built-in ReplyPen mirror only when GitHub returns 403 or 404 (use `--check` to see
`source: github` or `source: mirror`). HTTPS to that mirror is the trust root; the archive is still checked
against its mirrored `checksums.txt`. On macOS it runs the Homebrew updater; a standalone or duplicate setup requires the
explicit, idempotent `--migrate` path. Migration installs/upgrades the cask, verifies its version, removes
only binaries whose Go build metadata proves they are legacy replypen-cli installs, reshims mise, and
then verifies PATH selects the cask. Unknown binaries are reported for manual review, never deleted.

After installing or migrating, `which -a rp`, `rp --version`, `rp self update --check`, and
`rp self doctor` should all describe one PATH-visible binary at the latest published version.

## Sign in

`rp` authenticates with **OAuth**. Sign in once with `rp auth login`; it stores an access + refresh token in
`~/.config/replypen/tokens.json` (0600) and refreshes the short-lived access token transparently on
every later command. The token's **scope is chosen on the browser consent screen** (a single project,
one tenant under a project, or — for a global admin — all projects); there is no key to paste.
Tenant-enabled project logins can be project-scoped, then `--tenant <slug>` chooses the run scope per
command.

```bash
rp auth login            # opens your browser (PKCE loopback), catches the redirect, stores the token
rp auth login --device   # headless/SSH: prints a short code you approve in a browser on any device
rp auth logout           # revoke the token server-side and clear it locally
```

`rp auth login` prints the full sign-in URL before trying to open a browser, so agents can surface that URL
to a human immediately. If the local browser opener fails, the command keeps waiting on the printed URL;
use `rp auth login --device` for true headless/SSH sessions where a browser cannot reach localhost.

**Let the brain checkout select the profile.** A brain repo (`replypen-brain-<project>`) commits a
`.replypen.toml` binding it to one project, so `rp` run anywhere inside it first looks for
a local token profile with the same name. If that profile exists, it uses it; otherwise it uses the
`default` profile and sends the brain project as the server-side `--project` scope where supported:

```bash
cd replypen-brain-acme   # committed .replypen.toml: project = "acme"
rp auth login                  # stores a token under the "acme" profile
rp auth status                 # profile: acme · project: acme · tenant if bound · auth: logged in
rp ask "…"                # just works
```

That gives two workflows: project developers can keep one token per project profile, while a global
admin can keep one all-projects token in `default` and still have each brain checkout auto-scope to its
own project. Main intent: the checkout chooses the project context; the profile only chooses which
local token to use.

### Chat conversations — four rungs

A chat conversation is N runs sharing one session id. Each rung answers a different question, and each
one is deliberately cheap: reading a conversation should never cost you a megabyte of system prompt.

```bash
rp run sessions --tenant yes_events --limit 20   # WHICH conversations happened (newest first)
rp run thread <session_id> --transcript          # READ one conversation: every turn in order
rp run trace <run_id> --brief                    # ONE turn's details: question, answer, outcome, guards
rp run trace <run_id> --raw-output               # forensics: prompt, grounding, event timeline
rp run debug <run_id>                            # forensics on disk (jq-able JSONL + markdown index)
```

`rp run sessions` is the list view: created, tenant, turns, the feedback rollup (`5★` · `1★💬` · `-`),
the title or the opening question, and the session id to paste into `rp run thread`. Project-wide it
includes `tenant_slug`; `--tenant <slug>` narrows it. Narrow further with `--surface embed|dashboard`,
`--days N`, `--limit N`, and page with `--before <session_id>`; `-o json` is the server payload
verbatim, `next_before` included.

`rp run thread <session_id> --transcript` prints the conversation itself: per turn the timestamp, the
asserted principal, the question, the answer, the engine outcome, any human feedback and the run link.
A clarification FORM answer (a turn whose question starts with `User selected:`) is folded under the
turn it answers as `↳ formulier: …` and is not counted as a question — the footer says
`questions: N, form answers: M`. Past ~40 turns the answers are clipped to a head with a note, so a long
conversation never silently dumps. `-o json` gives
`{session_id, questions, form_answers, turns:[{seq, run_id, created_at, principal_id, question, answer,
outcome, feedback, run_url, is_form_answer}]}`; a folded turn carries the `seq` of the turn it answers.
Without `--transcript` the command keeps its pipeline view (channel outcome, runs, placement, health).

`rp run trace <run_id> --brief` is the same intent for ONE turn: question, prior messages, answer/notes,
outcome, `guards`, the egress summary and any tenant-settings drift — WITHOUT the system prompt, prompt
sections, manifest/bootstrap turns, grounding snapshots and the two tenant-settings blobs (on a real chat
turn those are ~90% of the bytes), and without the event timeline. Use `--raw-output` or `rp run debug`
the moment you are debugging rather than reading.

`rp run guards <run_id>` is the security readback: one row per checkpoint (injection scan, principal
scope, egress, output guard, output judge, final). When the chat confidentiality floor withheld the turn,
a `BLOCKED TURN` section follows: the text exactly as the guard/judge read it (PII-tokenized), the action
card it carried, and the judge's rationale + evidence quote — what you need to call a false positive. The
customer never saw it and no model reads it back. Needs a project-level admin token; `-o json` carries it
whole as `.blocked_turn`.

**Depth (Quick/Deeper).** Each chat turn is routed to `standard` or `pro`. `rp run trace` (both modes),
`rp run events` and `rp run debug` print it as `depth: pro (switched from standard; router: substantial
p=0.01)`; `--brief -o json` keeps it as `.depth`; `rp run sessions` adds a TIER column (`*` = member's
pick) once the server serves it. `rp project chat send` prints `tier:` after `run_id:`. To check routing
over a whole conversation, replay a script (one message per line, `#` skipped, `---` = new session):

```bash
rp project chat replay convo.txt --lane data --tier standard   # #, tier, secs, run_id, answer head
```

(`rp run session <share-link>` is a different thing: the account-less dump of a conversation someone
shared with you — see below.)

### Someone shared a link with you (no login)

A run link (`https://app.replypen.com/runs/<id>?t=…`) or a chat share link
(`https://app.replypen.com/s/<token>`) is itself the credential — no `rp auth login`, no profile:

```bash
brew install --cask replypen/tap/rp                           # or the curl installer above
rp run debug 'https://app.replypen.com/runs/<id>?t=<token>'        # → .replypen/debug/<run>-<project>.{md,jsonl}
rp run thread 'https://app.replypen.com/runs/<id>?t=<token>'       # the same run's thread + placement view
rp run trace 'https://app.replypen.com/runs/<id>?t=<token>'        # the raw bundle
rp run session 'https://app.replypen.com/s/<token>'                # → .replypen/debug/session-<id>.md
```

`run debug` writes the same two artifacts (markdown index + jq-able JSONL) a logged-in user gets.
`run session` writes ONE markdown file: the chat transcript in reading order, then the runs behind it,
each row ending in the `rp run debug` command that drills that run. Pass the token separately with
`--share-token <t>` if you have the id but not the link.

When `rp run debug|thread|trace <id>` is refused (no login, or a run your login cannot see) the error
carries a `hint:` line (a `hint` field in `-o json`) telling you to rerun with the full link — the
server answers a uniform 404 either way, so the hint is the only way to tell the two apart.

### Headless cloud agents

For a trusted personal cloud-agent environment, keep machine credentials project-bound and let each
checkout select its own named variable:

```toml
# .replypen.toml (committed; contains no secret)
project = "acme-staff"
machine_token_env = "RC_REFRESH_TOKEN_ACME_STAFF"

[[also]]                    # optional: a second project this checkout may act as
project = "acme-support"
machine_token_env = "RC_REFRESH_TOKEN_ACME_SUPPORT"
```

Tokens are one-project-only by design, so a checkout that must reach a second project declares an
`[[also]]` binding per extra project, each with its own separately minted token variable:

- no `--project` → the primary binding, exactly as before;
- `--project acme-support` (or `RC_PROJECT`) matching an `[[also]]` binding → that binding's profile,
  token variable and pin check, under the same provenance rules as the primary (no all-projects token
  needed); the marker/local `tenant` is not carried over, so pass `--tenant` if the second project needs one;
- `--project` naming anything else, or an explicit `--profile`, behaves exactly as it does today.

Each project and each variable may appear once; a duplicate is a config error. `rp auth status` shows the
selected binding (`binding_kind` / `binding_env` in `-o json`), and `rp auth access` notes a secondary
binding on stderr.

Store `RC_REFRESH_TOKEN_ACME_STAFF=<minted refresh token>` in the cloud environment. `rp` seeds the
`acme-staff` profile in its 0600 token store and refreshes the short-lived access token normally. A
single environment may hold several project-named variables; the current checkout's marker chooses the
profile. This reduces UI-selection mistakes, but it is not secret isolation: every session using that
environment can see every variable. Use separate environments for different users, trust boundaries, or
shared/public sessions.

Before any command endpoint runs, `rp` verifies that this credential is pinned to the marker's exact
project and rejects swapped or all-projects tokens. A cached machine credential is only used while its
provenance still holds: the checkout still declares the same `machine_token_env` and that variable is
set. Otherwise the stale cache is ignored (never sent) and local sessions continue with their normal
human OAuth login / `default` fallback — no manual token-store cleanup needed. In a Claude cloud session
(`CLAUDE_CODE_REMOTE=true`), a declared-but-missing variable fails closed instead of using a broader
`default` token.

For safety, a marker-sourced machine token is accepted only against built-in production
(`https://app.replypen.com`); use a separate `rp auth login` profile for staging/custom
`REPLYPEN_BASE_URL` targets.

Mint one token per project and add only the scopes the workflow needs:

```bash
rp project token mint email=agent@example.com \
  scope='read config:read runs:read console:db' \
  --project acme-staff -o json
```

The refresh token is shown once. Never commit it, pass it on argv, or use an all-projects token merely
to simplify cloud setup. `console:db` also requires the bound user to have the project's developer/admin
capability; omit it when production database reads are not needed. Add `runs:trigger`, `actions:read`,
or other scopes only when the agent actually uses those planes. Verify a fresh session with
`rp auth status -o json` and one narrow read.

Add `label=<text>` to `token mint` to name a credential. `rp project token ls` shows each token's last use
(time, IP, route), least recently used first, active tokens only (`--include-revoked` adds revoked ones,
`--wide` adds scope and user agent; `-o json` keeps the raw rows after the same filter and sort). Clean up stale
credentials with `rp project token ls --unused-days 90`, then `rp project token revoke <id>`. "never" means no
use since tracking began on 2026-10-12. `rp project token ls --all` (all-projects global-admin token) is the
fleet view: every project's machine credentials with owner email and status, `--include-revoked` adding the
revoked/expired/superseded ones from the last 30 days.

### Cloud setup

The hosted setup script installs or updates `rp`, uv, and pnpm from checksum-verified, cloud-reachable
sources. `rp` always tracks the mirror's latest release; uv and pnpm follow a **minimum-version** policy
(`UV_MIN`/`PNPM_MIN` in the script — currently uv >= 0.8, pnpm >= 10): a preinstalled build that already
satisfies the minimum is left alone, otherwise the pinned build is installed over the shadowing binary
when its directory is writable, so it actually wins on `PATH`.

```bash
curl -fsSL https://app.replypen.com/install/cloud.sh | bash
```

The final summary marks each tool `(preinstalled)` or `(installed)`.
Set `RC_CLOUD_SKIP_UV=1` or `RC_CLOUD_SKIP_PNPM=1` to omit an optional tool. `RC_RELEASE_MIRROR`
overrides the rp release mirror. Platform detection affects only the log label: Claude cloud is detected
from `CLAUDE_CODE_REMOTE=true`; set `RC_CLOUD_PLATFORM=codex` for Codex/ChatGPT cloud, or explicitly use
`claude`/`generic`. Add `app.replypen.com` and `*.amazonaws.com` to the environment network allowlist;
keep its common-package-manager hosts enabled for uv and pnpm. The redirect accepts `?v=vX.Y.Z` when a
reviewed setup-script release must be pinned.

Project-brain publishing is exact and OAuth-only: push the tested commit to GitHub, run `rp dev brain
sync`, then `rp dev brain promote --channel stable|edge --sha <full-40-character-SHA>`. On a templated
(multi-tenant) project you can see the promotion's verdict first with `rp dev brain preflight --sha
<SHA> [--channel stable|edge]`: the server compiles that commit for every tenant pinned to the channel and
lists any that would break or fall back to placeholder text, without moving anything (non-zero exit when
it would). Promote runs the same check itself and refuses a bad candidate, so preflight is optional. Promotion moves
only the named managed channel to that commit; it never promotes an ambient `main` tip. Finish with `rp
dev brain status` and confirm the channel's **resolved** SHA. `rp dev brain publish --channel … --sha …`
does the whole choreography in one gated command — sync, promote, then verify the channel resolved to
that exact SHA — and exits non-zero on any mismatch (`-o json` carries the receipt). Tenant overlays always run from their own
`main` and have no promotion route; a push alone is not picked up, so publish one with `rp dev brain publish --sha
<SHA>` (no `--channel`, from the tenant checkout or with `--tenant`): it syncs the overlay and proves the served
`main` contains that commit. A tenant-scoped login cannot move the shared project channel; sign
in with an authorized project-maintainer login instead. Explicitly narrowed tokens need the dedicated
`brain:promote` OAuth scope in addition to that project-level admin authority.

**Source mirrors** follow the same rule: a push is picked up by the hourly refresh at best. From the
source repo's checkout, after pushing the commit to every branch a project tracks, `rp dev mirror publish
--sha <SHA>` refreshes every mirror of that repo your login can see (`--project` narrows it), each at its
tracked branch's tip, proves that tip contains the SHA, and refreshes warm console sessions. It refuses
before refreshing anything when a tracked branch lacks the commit, and never pushes.

**Read-only brain alias.** A project can run on another project's brain (its on-box `main`) while keeping
its own secrets, databases, chat config and runs. Operator-only: `rp project settings runtime set
brain_source_project_id=<source-project-uuid>` (`brain_source_project_id=` clears). Brain writes on the alias
(edit/consolidate/promote/publish) fail with 409 `BRAIN_READ_ONLY_ALIAS`; `rp dev brain status` shows the `Source:`.

To eyeball what one tenant actually gets from a templated brain, run `rp dev brain render --tenant <slug>
[--path AGENTS.md] [--all] [--sha <SHA> | --channel stable|edge]`. The server compiles that tenant's
placeholders and `rp:branch` regions in memory (the deployed brain cache is untouched) and returns the
files exactly as `/brain` would mount them: a short header with the resolved commit, the fill/branch/
degradation counts, then each file under a `=== <path> ===` marker. Without `--sha`/`--channel` it uses
the tenant's currently resolved channel. Where preflight answers "would this commit break anyone",
render answers "what does this tenant read". Large output spills to
`.replypen/output/brain-render-<tenant>/`, keyed by tenant only: rendering a second sha/channel
overwrites the first, so move it aside if you want to compare.

Tenant-brain repository access also stays server-side: from a tenant brain checkout, run
`rp dev brain developer invite <github-handle>`. The ReplyPen GitHub App creates or reuses the
repository invitation, and human output prints its acceptance URL; `-o json` returns the raw receipt.
Outside a checkout, select the exact repository with `--project <project> --tenant <slug>`.

**Base URL** is deliberately boring: production is hardcoded to `https://app.replypen.com`. The only
runtime override is `REPLYPEN_BASE_URL`, for deliberate staging/dev work. `rp auth login` uses the same
resolution, and `rp auth status` prints both the URL and its source (`built-in production` or
`REPLYPEN_BASE_URL`). Old `base_url` values in `~/.config/replypen/config.toml`, `.replypen.toml`,
or the token store do not steer normal commands. The legacy production host
`https://rootcause.probackup.io` is treated as an alias when an explicit env/token URL is canonicalized.

```bash
export REPLYPEN_BASE_URL=https://staging.your-replypen-host
```

**Profiles** are the token-store keys. The profile is resolved as: explicit `--profile <name>` >
the brain marker's project if that token exists > `"default"`. `--profile` picks *which stored token*
a command uses. Concurrent `rp` calls on one profile are safe: the token refresh runs under a
per-profile file lock, so parallel processes share one renewal instead of racing the single-use refresh
token, and different profiles refresh independently.
`--project <id-or-name>` is **not** a token selector — it's a **server-side scope**: it keeps the active
token and names one project on supported endpoints (`?project=`), so an **all-projects admin token** can
review a single project (`rp fleet runs --project momentum-tools`) or trigger one (`rp ask --project
momentum-tools "…"`) without minting a per-project profile; a project-pinned token rejects any selector
that does not name its own project.
When a project scope is set, the CLI validates it against `rp project list` first and fails typos with a
hint to run `rp project list`.
On tenant-enabled projects, the active `rp auth login` may bind one tenant or the whole project. Plain
`rp ask "…"` works when the token is tenant-pinned; project-pinned logins pass `--tenant <slug>` per
workspace-producing command. Every executable command declares whether it applies project/tenant scope;
unsupported selectors fail before a request, and `--all` cannot be combined with either. Human
tenant-scoped output starts with `Scope: project / tenant`; JSON remains the raw API response. `rp auth
status` shows the login-bound project and tenant, when one is pinned.

For a whole-fleet review with an all-projects token, `rp fleet runs`, `rp fleet patterns`, and
`rp fleet health` take **`--all`**. The CLI lists the fleet (`rp project list`) and fans out per project —
`fleet runs --all` groups the digest by project with a fleet total, `fleet health --all` exits non-zero
if ANY project is unhealthy, and `fleet patterns --all`
clusters per project. `-o json` emits the merged `{projects:[…]}` shape. `--all` against a project-scoped
token is a friendly error (it needs an all-projects token), not a silent single-project run.

`rp fleet runs` also carries **`--timeline`** (per-day runs/errors/latency), off by default to keep the
digest scannable; the per-run rows always ride in `-o json` so the breakdown is re-derivable. Stuck runs
(`running` past a 30m clock with no finish) and a `FB` fallback flag are surfaced inline; every
worst-offender line carries the full triage tail (secs · turns · bash_err · FB). Heaviness is read off
**turns / bash / duration** — the CLI shows no spend or token counts on any surface, and no **serving
model identity** either: naming the rung that answered is as cost-reverse-engineerable as naming the
provider that served it, so the API projects none of it on any tier. `is_fallback` survives as the
content-free half — THAT the loop swapped rungs, never between which models. The model×cost×fallback
breakdown is a superadmin `db.py` query now, not a `rp` view.

`rp fleet actions` is the operator-only cross-run action index. It automatically pages a recent window
without downloading the much larger event feed or calling every run separately. Repeat `--action` and
`--status` to OR exact filters; statuses are `proposed`, `executing`, `succeeded`, `failed`, and
`canceled`. Human output includes each action-run/run id, timestamps and duration, exact structured
params, and the full freshly tokenized run URL by default. A settled failure renders as a `CLASS` column
plus an `Error:` detail line: the host-stamped infra classes (`executor_predispatch` — provably nothing
ran, retry-safe; `executor_error`, `no_executor`, `no_runner_url`, `attachment_fetch`) mean ReplyPen's
own machinery failed, anything else is the action's own domain refusal. The message is clamped to one
readable line in table/agent output. Piped/`-o json` output preserves the complete raw action rows —
including the untruncated `error_message`; explicit `--format agent` pins a one-line-per-action complete
view even through a pipe. `rp run actions` shows the same class column, narrowed server-side to the host
vocabulary (an action-authored class collapses to `action_error`) and without the message, which can echo
customer data.
The endpoint needs `console:action` plus operator/admin action-view authority because grounded params may
contain customer values.

Use bare **`--learning`** to keep runs with any dream-cycle signal, or select one with
`--learning=feedback|sent_delta|triage_skipped|triage_corrected`; the same filter works on
`rp run list`. Run-list tables show the matching safe boolean signals, and fleet tables/agent output
carry them as `LRN:` flags. Shadow sent deltas render as `LRN:sent_delta/<verdict>` (or
`LRN:sent_delta/unjudged`); equivalent and same-outcome rows stay out of the failure shortlist. JSON
keeps the server rows unchanged, including `sent_delta_shadow` and `sent_delta_verdict`.

Use **`--reviewed`** on `rp fleet runs` or `rp run list` for the human-audit corpus: every run with a
1–5 review score, including held-out eval runs. Operator JSON includes `review.score` and the optional
`review.comment`; this stays separate from `--learning=feedback`, which excludes held-outs.

`rp run feedback list` is the feedback-first view: one row per rated/commented run, newest rating
first, windowed on the rating time (`--days`/`--since`/`--until`) rather than run creation. Narrow with
`--score negative|positive`, `--kind`, `--surface`; drill with `rp run show <run_id>` or, for chat,
`rp run thread <session_id> --transcript`.

`rp dev learning evidence` is intentionally JSON-only because feedback, deltas, and triage evidence
have different wire shapes. `--plane feedback|deltas|triage` narrows the corpus; `--plane shadow` is a
convenience alias for `--plane deltas --shadow=true`. Use `--shadow=true|false`, `--verdict <csv>`, and
`--days N` for a bounded comparison corpus. Delta and question bodies stay omitted unless
`--include-bodies` is explicit; JSON is the unmodified server response.

**`.replypen.toml`** (committed, per brain) names the project + endpoint — no secret, safe to commit,
ships the binding with a clone. Optional gitignored **`.replypen/local.toml`** can still set a local
tenant override for debugging. There is no longer any `.replypen.secret.toml` — credentials live only
in the OAuth token store.

## Commands

The inventory below is generated from Cobra's offline command tree. Refresh it together with recursive
help using `go test ./internal/cli -update`.

<!-- BEGIN GENERATED COMMAND INVENTORY -->
| Command | Purpose |
|---|---|
| `rp admin catalog ls` | List catalog |
| `rp admin catalog upsert` | Create or update a catalog entry (keyed on key=) |
| `rp admin catalog` | Manage the integration catalog |
| `rp admin project add` | Create a project (name=… [default_tier=standard|pro] [egress_mode=wildcard|enforce]) |
| `rp admin project identity-failures` | List identity failures without patient details (global admin) |
| `rp admin project ls` | List projects |
| `rp admin project` | Manage box-level projects |
| `rp admin user add` | Create a user (email=… [admin=true] [password=…]) |
| `rp admin user ls` | List users |
| `rp admin user set` | Update a user ([admin=true|false] [password=…]) |
| `rp admin user` | Manage box-level users |
| `rp admin` | Box-level administration (users/projects/catalog; global-admin token) |
| `rp ask` | Trigger a run from a question and wait for the answer |
| `rp auth access` | Show what this token may do (scopes, writable keys, resources) |
| `rp auth login` | Sign in with OAuth (PKCE loopback by default, --device for headless) |
| `rp auth logout` | Revoke and clear this profile's stored tokens |
| `rp auth status` | Show the resolved profile/project/login tenant + sign-in status |
| `rp auth` | Manage local authentication and inspect access |
| `rp dev action doctor` | Diagnose action resolution, Embassy health, and preflight |
| `rp dev action` | Diagnose project actions |
| `rp dev api openapi` | Dump the canonical OpenAPI document |
| `rp dev api routes` | Show the canonical API route manifest |
| `rp dev api` | Inspect the public API contract |
| `rp dev brain consolidate` | Queue the consolidation cron on demand |
| `rp dev brain developer invite` | Invite a GitHub user to this tenant brain repository |
| `rp dev brain developer` | Manage tenant brain developer access |
| `rp dev brain edit` | Queue a brain edit from a plain-language instruction (or STDIN) |
| `rp dev brain preflight` | Dry-run a channel promotion: which tenants would the candidate commit break? |
| `rp dev brain promote` | Promote an exact tested commit to a project brain channel |
| `rp dev brain publish` | Make an exact pushed commit live: promote a project brain channel, or sync a tenant brain |
| `rp dev brain render` | Show a tenant's compiled brain projection |
| `rp dev brain status` | Show deployed brain cache status |
| `rp dev brain sync` | Fetch origin/main and refresh deployed brain cache |
| `rp dev brain` | Inspect, publish, and manage brain repositories |
| `rp dev console action list` | List available actions |
| `rp dev console action preflight` | Run the action's preflight.py in the console workspace, then the Embassy dry-run |
| `rp dev console action run` | Execute an action |
| `rp dev console action show` | Show one action manifest |
| `rp dev console action` | Inspect and execute guarded ReplyPen actions |
| `rp dev console bash list` | List cataloged brain scripts |
| `rp dev console bash run` | Run one command in the guarded workspace console |
| `rp dev console bash` | List or run workspace console commands |
| `rp dev console capabilities` | List direct production primitives available to this login |
| `rp dev console database list` | List available databases |
| `rp dev console database query` | Run a guarded production database query |
| `rp dev console database schema` | Fetch database schema, optionally one table |
| `rp dev console database` | Access guarded production databases |
| `rp dev console file get` | Download a sandbox file or directory without the bash output cap |
| `rp dev console file` | Transfer files from a guarded workspace console |
| `rp dev console paths` | Check which absolute paths exist in a production workspace |
| `rp dev console` | Use guarded production consoles |
| `rp dev context-export` | Render a project's full opening context offline (needs a ReplyPen host checkout) |
| `rp dev learning evidence` | List feedback, sent-delta, shadow, and triage evidence |
| `rp dev learning` | Inspect learning and consolidation inputs |
| `rp dev mirror publish` | Refresh every project mirroring this repository and verify each contains a pushed commit |
| `rp dev mirror refresh` | Refresh mirrors and verify one repository at an exact commit (--tenant for a tenant's own mirror) |
| `rp dev mirror` | Refresh project or tenant source mirrors |
| `rp dev onboarding goto` | Re-seed an onboarding world at a step and print the URL to open |
| `rp dev onboarding` | Drive onboarding worlds on a local make dev server |
| `rp dev platform-skills pull` | Cache the deployed platform skill library locally (a no-op when already current) |
| `rp dev platform-skills` | Pull the platform skills runs mount at /skills |
| `rp dev replies feedback` | Rate one proposed reply (score 1-5 and/or comment, saved independently) |
| `rp dev replies list` | List a tenant's answered mails, newest first |
| `rp dev replies` | Review a tenant's answered mails and rate the proposed replies |
| `rp dev tools draft-render` | Render draft markdown to the HTML ReplyPen places in a mail draft |
| `rp dev tools id gmail` | Translate Gmail hex/decimal/thread-f: ids + build a clickable URL |
| `rp dev tools id outlook` | Classify an Outlook/Graph id + tell you which DB column matches it |
| `rp dev tools id` | Translate provider message/thread ids |
| `rp dev tools provider detect` | Detect a domain's email backend (google/microsoft/other) from DNS |
| `rp dev tools provider` | Provider (channel) utilities |
| `rp dev tools` | Use provider, identifier and draft-render utilities |
| `rp dev` | Develop and inspect project behavior |
| `rp fleet actions` | Find actions across recent runs with exact params and run URLs |
| `rp fleet deploy-state` | Show the live host, brain, and mirror SHAs with their history |
| `rp fleet health` | Roll up project health (mirrors, brain boot checks, dead-letters, PII-proxy withheld columns); exits non-zero when unhealthy |
| `rp fleet patterns` | Cluster recent failures and outbound endpoint patterns |
| `rp fleet runs` | Fleet digest of recent runs (flags, rates, worst offenders) |
| `rp fleet` | Operate and inspect project health |
| `rp project action draft approve` | Approve an action draft |
| `rp project action draft new` | Scaffold a new action draft |
| `rp project action draft show` | Show an action draft |
| `rp project action draft submit` | Submit an action draft |
| `rp project action draft test` | Mint a human-confirmed test run for an action draft |
| `rp project action draft validate` | Validate an action draft |
| `rp project action draft` | Create, inspect, test, and promote action drafts |
| `rp project action-settings get` | Show current values (value / effective / default) |
| `rp project action-settings probe` | Probe the configured Embassy from ReplyPen |
| `rp project action-settings reverse-secret rotate` | Generate, store, and print a new reverse-channel secret once |
| `rp project action-settings reverse-secret` | Manage the Embassy reverse-channel secret |
| `rp project action-settings set` | Change values (sparse, validate-then-apply server-side) |
| `rp project action-settings` | Read, change, and probe action-plane configuration |
| `rp project action` | Author action drafts |
| `rp project branding get` | Show current values (value / effective / default) |
| `rp project branding logo clear` | Remove the stored logo |
| `rp project branding logo set` | Upload a logo image (PNG/SVG/JPEG) |
| `rp project branding logo` | Set or clear the white-label logo image |
| `rp project branding set` | Change values (sparse, validate-then-apply server-side) |
| `rp project branding` | Read or change white-label branding (colours/name/public_base_url) |
| `rp project chat attachment` | Download one file of your own conversation (e.g. a review screenshot) |
| `rp project chat brief` | Print the secret-free chat implementation brief |
| `rp project chat card` | Print a chat card's current projection (--wait polls until it settles) |
| `rp project chat decide` | Settle a chat card (e.g. publish or decline a website change) and print its projection |
| `rp project chat doctor` | Diagnose embedded-chat configuration and recent rejects |
| `rp project chat follow` | Stream a still-running turn (e.g. a card's review_run_id) like send |
| `rp project chat get` | Show current values (value / effective / default) |
| `rp project chat replay` | Replay a file of user messages as one chat session and tabulate tier, time and answer per turn |
| `rp project chat secret reveal` | Reveal the chat signing secret (printed once) |
| `rp project chat secret rotate` | Rotate the chat signing secret (printed once) |
| `rp project chat secret` | Manage the dedicated chat signing secret |
| `rp project chat send` | Send one chat turn and print its SSE frames and session + run IDs |
| `rp project chat session` | Print a session's persisted transcript as the widget would reopen it |
| `rp project chat set` | Change values (sparse, validate-then-apply server-side) |
| `rp project chat token` | Mint a five-minute server chat token |
| `rp project chat` | Configure, diagnose, and smoke-test embedded chat |
| `rp project connection add` | Create a connection |
| `rp project connection ls` | List connections |
| `rp project connection probe` | Probe an integration capability grant |
| `rp project connection reveal` | Print a connection's secret (sensitive, shown once) |
| `rp project connection rm` | Revoke and delete a connection |
| `rp project connection rotate` | Rotate a connection's secret |
| `rp project connection` | Manage outbound integration connections |
| `rp project corpus download` | Download an export artifact (Markdown corpus or templates JSON) |
| `rp project corpus get` | Show one export |
| `rp project corpus ls` | List exports (id, kind, format, status, items, truncated, created/completed) |
| `rp project corpus mine-settings` | Mine a completed harvest for persona/triage setting proposals |
| `rp project corpus` | Read mailbox exports (harvest/survey/templates) |
| `rp project database controls get` | Show a database's controls |
| `rp project database controls set` | Change a database's controls (JSON object or k=v pairs; sparse) |
| `rp project database controls` | Read or change a database's access controls |
| `rp project database get` | Show one database |
| `rp project database ls` | List databases |
| `rp project database preview` | Preview the scoped rows a (tenant, principal) would see |
| `rp project database set` | Update a database |
| `rp project database` | Manage registered databases (list/read/update + access controls) |
| `rp project egress` | Inspect outbound endpoints, volume, and unattributed traffic |
| `rp project env diff` | Compare local ./.env to the server (NAMES-only drift); nonzero exit on drift |
| `rp project env keys` | List grounding env key NAMES (log-safe, no values); --tenant lists the tenant's own |
| `rp project env pull` | Fetch the production grounding env and write ./.env (0600); prints NAMES + count, never values |
| `rp project env reveal` | Print one env var's value (sensitive, shown once) |
| `rp project env rm` | Delete one env var |
| `rp project env set` | Upsert one env var (value from STDIN by default; never echoed) |
| `rp project env` | Manage this project's sealed env secrets |
| `rp project github status` | Show the GitHub App install status (installed/account/install_url) |
| `rp project github` | Inspect the GitHub App install for this project |
| `rp project knowledge article apply` | Apply a help-centre article block to its provider |
| `rp project knowledge article get` | Fetch one help-centre article as an applyable markdown block |
| `rp project knowledge article` | Write and read help-centre articles from a markdown block |
| `rp project knowledge content export` | Export selected KB articles to a fresh local artifact directory |
| `rp project knowledge content list` | List KB collections without article bodies |
| `rp project knowledge content search` | Search KB articles and write matched articles to local artifacts |
| `rp project knowledge content` | List, search, and export knowledge articles |
| `rp project knowledge summaries apply` | Check reviewed summaries against the synced KB and queue the sync that applies them |
| `rp project knowledge summaries` | Replace synced KB article summaries with reviewed ones |
| `rp project knowledge sync get` | Show current values (value / effective / default) |
| `rp project knowledge sync set` | Change values (sparse, validate-then-apply server-side) |
| `rp project knowledge sync` | Manage knowledge synchronization settings |
| `rp project knowledge` | Search knowledge content and configure synchronization |
| `rp project list` | List the projects this token can see (the fleet, for an all-projects token) |
| `rp project mailbox connect-imap` | Connect a generic IMAP/SMTP mailbox (live-probed before it's saved) |
| `rp project mailbox connect` | Print the dashboard Connections URL to start a provider's browser OAuth |
| `rp project mailbox harvest` | Start a local-synthesis harvest of a mailbox (optionally wait for the export) |
| `rp project mailbox imap-env` | Write an IMAP mailbox env file for local deep harvest (0600; values never printed) |
| `rp project mailbox ls` | List watched mailboxes (id, provider, email, status, mode, tenant, health) |
| `rp project mailbox mode` | Set the mailbox watch, processing, and delivery mode |
| `rp project mailbox password-link` | Print the no-login password link for an IMAP mailbox (also used for rotation) |
| `rp project mailbox seed-imap` | Store an IMAP mailbox's server settings without a password and print its no-login password link |
| `rp project mailbox settings get` | Show settings with resolved provenance |
| `rp project mailbox settings set` | Patch settings (nested; key= or --unset clears local override) |
| `rp project mailbox settings` | Read or edit nested mailbox settings (persona/channel) |
| `rp project mailbox templates` | Export Gmail canned responses as a separate JSON artifact |
| `rp project mailbox test` | Re-run the live IMAP/SMTP check on a connected mailbox and print the checklist |
| `rp project mailbox` | Manage watched mailboxes (the channel plane's inbox watch) |
| `rp project member add` | Create a member |
| `rp project member ls` | List members |
| `rp project member rm` | Delete a member |
| `rp project member` | Manage project members |
| `rp project principals get` | Show the principal manifest |
| `rp project principals resolve` | Resolve an email or pass through an external principal ID |
| `rp project principals set` | Replace the validated principal manifest |
| `rp project principals` | Read or replace the project principal manifest |
| `rp project rename` | Rename the active project slug and brain repo |
| `rp project repo add` | Create a repo |
| `rp project repo ls` | List repos |
| `rp project repo rm` | Delete a repo |
| `rp project repo set` | Update a repo |
| `rp project repo` | Manage source repos (mirrors + per-repo PR config) |
| `rp project review close` | Close a queue card already handled elsewhere, with a note saying where (commits, thread) |
| `rp project review decide` | Decide a card: one of its option labels (see show), Other or Skip, plus an instruction |
| `rp project review finish` | Finish the current review (a weekly review applies its decisions; the queue only acknowledges) |
| `rp project review ls` | List open review cards of one audience |
| `rp project review show` | Show one review card (also once applied or closed) with its options and review-page link |
| `rp project review` | Work the review queue: list, show, decide, close or finish cards (like the Review page) |
| `rp project senders allow` | Never treat mail from <pattern> as spam |
| `rp project senders block` | Always treat mail from <pattern> as spam |
| `rp project senders ls` | List the spam allow and block rules |
| `rp project senders rm` | Delete a spam rule by id |
| `rp project senders` | Manage the spam allow/block lists (never-spam / always-spam) |
| `rp project settings behavior get` | Show settings with resolved provenance |
| `rp project settings behavior set` | Patch settings (nested; key= or --unset clears local override) |
| `rp project settings behavior` | Read or edit nested project settings (persona/channel) |
| `rp project settings describe` | Explain one config key (type, enum, scopes, default, help) |
| `rp project settings proposals approve` | Approve and apply a settings proposal |
| `rp project settings proposals ls` | List settings proposals (pending first, then recently decided) |
| `rp project settings proposals reject` | Reject a pending settings proposal |
| `rp project settings proposals show` | Show one settings proposal with its payload and evidence |
| `rp project settings proposals` | Review proposed settings changes (list, approve, reject) |
| `rp project settings runtime get` | Show current values (value / effective / default) |
| `rp project settings runtime set` | Change values (sparse, validate-then-apply server-side) |
| `rp project settings runtime` | Manage flat runtime settings |
| `rp project settings schema` | Show the config registry (fields, types, scopes, defaults) |
| `rp project settings` | Read, change, and describe project settings |
| `rp project tenant add` | Create a tenant |
| `rp project tenant budget add` | Raise (or with a negative amount, lower) a tenant's monthly AI limit by USD |
| `rp project tenant budget get` | Show a tenant's monthly AI limit and this month's usage |
| `rp project tenant budget ls` | List every tenant's monthly AI limit and this month's usage |
| `rp project tenant budget set` | Set (or create) a tenant's monthly AI limit as an absolute USD amount |
| `rp project tenant budget` | List, create or change tenants' monthly AI limits in USD (project admins) |
| `rp project tenant get` | Show one tenant |
| `rp project tenant ls` | List tenants |
| `rp project tenant profile get` | Show a tenant's projection/profile values |
| `rp project tenant profile schema` | Dump the tenant profile JSON schema |
| `rp project tenant profile set` | Edit tenant projection/profile values |
| `rp project tenant profile` | Read or edit tenant projection/profile values |
| `rp project tenant set` | Update a tenant |
| `rp project tenant settings get` | Show settings with resolved provenance |
| `rp project tenant settings schema` | Dump the hierarchy settings schema (debug/reference) |
| `rp project tenant settings set` | Patch settings (nested; key= or --unset clears local override) |
| `rp project tenant settings` | Read or edit nested tenant settings (persona/channel) |
| `rp project tenant` | Manage tenants (sub-scopes below a project) and their settings |
| `rp project token ls` | List tokens with last-used info (active only, least recently used first) |
| `rp project token mint` | Mint a new token (refresh token shown once) |
| `rp project token revoke` | Revoke a token |
| `rp project token` | Manage API tokens |
| `rp project triage eval` | Run the brain's triage case corpus through the real triage decision at a brain ref |
| `rp project triage policy get` | Show triage policy guidance |
| `rp project triage policy set` | Replace triage policy guidance |
| `rp project triage policy` | Read or edit free-form triage guidance |
| `rp project triage rules add` | Create a triage hard rule |
| `rp project triage rules ls` | List triage hard rules |
| `rp project triage rules rm` | Delete a triage hard rule |
| `rp project triage rules set` | Patch a triage hard rule |
| `rp project triage rules` | Read or edit deterministic triage rules |
| `rp project triage` | Read or change mail triage policy and hard rules |
| `rp project` | Manage project configuration and resources |
| `rp run actions` | Show the safe action lifecycle history |
| `rp run attachments` | List or download the files in a chat session |
| `rp run brain-diff` | Show the brain commit written by a run |
| `rp run debug` | Decompose a run into local debug artifacts |
| `rp run egress` | Show outbound gateway connections and HTTP attempts |
| `rp run events` | Show the full per-event trace |
| `rp run feedback list` | List runs that received a human score or comment, newest rating first |
| `rp run feedback` | Record score/comment feedback on a run's trace, or mark it processed; `list` reviews rated runs |
| `rp run guards` | Show every security-checkpoint verdict for one run |
| `rp run list` | List recent runs (filterable) |
| `rp run process-thread` | Process a triage-skipped or security-blocked inbox thread |
| `rp run retry` | Re-run a run; --comment steers the rerun with reviewer guidance (e.g. the corrected answer) and lets it keep the original tier; without it a finished run escalates one tier |
| `rp run session` | Dump a shared chat session (transcript + its runs) to one markdown file |
| `rp run sessions search` | Find chat sessions by principal, creation date and prose |
| `rp run sessions` | List chat conversations (sessions) with turns, outcome and feedback |
| `rp run show` | Show one run |
| `rp run thread` | Trace one run, provider/local thread, or session through pipeline and placement |
| `rp run trace` | Show the whole run bundle (--brief: just what was asked and answered) |
| `rp run` | Inspect and manage the run lifecycle |
| `rp self completion` | Generate a shell completion script |
| `rp self doctor` | Diagnose the active rp install, PATH copies, scope, and updates |
| `rp self update` | Update rp to the latest release (self-update) |
| `rp self` | Manage the rp installation and shell integration |
| `rp status` | Health summary + recent runs |
<!-- END GENERATED COMMAND INVENTORY -->

Global flags: `--profile <name>` picks the stored token; `--project <id-or-name>` scopes supported
requests to one project server-side and is validated against `rp project list` before use (useful for
all-projects tokens outside a brain checkout or as an override; inside a brain checkout the
`.replypen.toml` project is used automatically when falling back to `default`);
`--tenant <slug>` explicitly selects a tenant where supported; it is required for workspace-producing
commands when a tenant-enabled project login is project-pinned. `--scope project|tenant` forces request
routing: `project` clears any resolved tenant (a `--tenant`, a brain checkout, or a tenant-bound login)
so a tenant-capable command hits the project route; `tenant` requires a resolvable tenant. `-o json|table`
forces output. Large output spills to `.replypen/output/` by default; use `--out-dir <dir>` or
`RC_OUTPUT_DIR` to change that, `--no-preview` to print paths/metadata only, and `--raw-output` to
preserve exact full stdout.

`rp ask --brain-ref dev/<branch>` runs the question against a **non-main brain ref** — the project
dev's "test without pushing main" loop. Push a `dev/*` branch to your brain first (`git push origin
dev/<branch>`); the server runs the real loop against it and flags any actions/PRs as test.

`rp ask --mirror-ref <repo>=<ref>` (repeatable) mounts that `dev/*` branch or SHA of a source mirror for this run
only; like `--brain-ref` it makes a test run (journal suppressed). An unknown mirror name or malformed ref
is a 400 `BAD_MIRROR_REF`; a ref missing on the mirror's origin fails the run. `rp run debug` lists each
mirror's `ref @ sha` under Projection inputs, marked `override` for these mounts.

`rp ask --simulation` is a **dress rehearsal**: the run rides the tenant's normal production brain and
real grounding, but every side effect is fenced off — action autonomy is clamped to human (proposals
only), the durable journal commit is suppressed and the result is never placed in a mailbox. Use it to
show a customer how their configuration would answer. Attachments are refused on a simulation (they mint
a thread on the tenant's live mailbox). The API echoes `simulation` on `GET /api/v1/runs/{id}` and on
each run of `GET /api/v1/runs`, which also accepts `?simulation=true|false`.

`rp ask --replay-chat-run <run id|run link> [question]` replays one stored **in-app chat turn** (a
`kind=chat` run) with its real chat framing, prior turns, hidden context and verified principal, as a
simulation; the original conversation is untouched (no transcript write, no hot container, no cards).
A question replaces that turn's message ("what if they had asked it like this"). Pair it with
`--brain-ref` / `--mirror-ref` to test a fix against the exact turn. It needs the grant `rp run debug`
needs on that run (project admin, or the developer who created it); `--principal-*`, `--file`,
`--scenario`, `--from`, `--subject` and `--session` are refused. `rp run show` / `run debug` print
`Replay of: <run id>`. Errors: 404 `UNKNOWN_RUN` (not visible), 403 `FORBIDDEN` (no full-trace grant),
409 `NOT_REPLAYABLE` (not a chat turn, a "Set up & improve" turn, or its input/conversation expired).

`rp ask` defaults to `--scenario email`: it sends an explicit `scenario=email` to the Prompt API and
wraps the prompt as one synthetic inbound message from `--from` with `--subject` (or a compact prompt
first line). Use this for high-fidelity brain-dev checks: tone, notes, actions, PR proposals, and
declines are rendered like a reviewable support result. When the run deliberately addresses a new
message, `ask`, `run show`, `run trace`, and `run debug` show its to/cc/bcc recipients and subject;
JSON carries the optional `outbound_email` object verbatim. Use `--scenario raw` for direct
investigations; the CLI sends `scenario=raw` and prints one Markdown answer.

`rp ask --file path/to/file.pdf` uploads a local file as an inbound attachment on the synthetic
message. Repeat it for multiple files; relative paths are resolved from the current working directory.
Caps are checked locally before upload and mirror the server's: **max 4 files, 5 MiB each, 15 MiB
total**. `--attach` is a deprecated alias for `--file` (still merged into the same list).
The backend gives each file a real `attachment_id`, so hosted actions with `type: attachment` params can
be proposed against the same ID shape as production email. Action proposal/execution still depends on
the project's action plane and catalog being enabled.

`rp ask --effort pro` is a per-run escalation knob (`max` is still accepted and reads as `pro`). It maps to ReplyPen's model tiers, not raw
provider effort values; use it when you explicitly want a stronger retry. Omit it, or pass
`--effort default`, for normal behavior.

`rp ask --credential-env NAME` (repeatable) sends the local env var `NAME` as a run **credential** —
an integrator-minted, user-scoped token the host seals for this run only and injects into the
workspace as `NAME` (the Prompt API `credentials` member, same rules as the chat embed claim). The
value is read from your environment, never argv. On `--simulation` / `--replay-chat-run` the host only
accepts names the project lists in `behavior.simulation_credentials`.

`rp ask --principal-kind <K> --principal-id <ID>` asserts a **principal** on the run — a structured
identity that scopes the run's read-only data access to that entity's rows (e.g.
`--principal-kind kampadmin_person --principal-id <person-uuid>`). Both flags are a pair (supply both
or neither); `--asserted-by`/`--assurance` optionally refine the assertion and require the pair. The
principal is dormant unless the project declares `scope_claims`; without that config the server discards
it. Tenant binding stays the explicit `--tenant` slug — it is not part of the principal. A
principal-bearing submit never falls back to the legacy body, so a stale server rejects it rather than
silently dropping the scope.

#### `--session` — multi-turn threading

`--session <id>` threads a run onto a session so follow-up turns build on the earlier ones. The id is
**client-chosen** (any stable string — `session_id` is the join key, not `run_id`); reuse the same id
across turns and the server warm-starts each follow-up off the prior turns on that session:

```bash
rp ask --session homer-42 "Hello, my name is Homer. When can I have a dental appointment?"
rp ask --session homer-42 "Okay, let's go for Thursday."   # same session → warm-started
```

The server keys continuity on `(project, session_id, kind=prompt)` and injects a digest of **what the
earlier turns ran and how each ended** (the command trail) — *not* the prior conversation or answer
text. So a follow-up knows which queries already dead-ended, but does **not** yet replay the prior
turn's answer; phrase follow-ups to stand on their own rather than referring back anaphorically ("the
second option"). `--no-wait` prints just the `run_id`; the `session_id` is the one you passed in.

Output auto-detects: **TTY → table, piped → JSON**. Force with `-o json` / `-o table`. API errors are
surfaced verbatim (`CODE: message`) with a non-zero exit. `RC_PROFILE`, `RC_PROJECT` and `RC_TENANT` provide
the defaults for their matching global flags. HTTP requests time out after 10 minutes by default
(`RC_HTTP_TIMEOUT=2m` overrides it); safe reads retry 429/5xx responses three times.

### Composable console primitives

Inline database reads stay capped at 500 rows. A truncated inline result fails closed with exit 3;
use `--allow-truncated` only when a partial answer is intentional, or `--all` to stream every row.
An `--all` query is one HTTP response backed by a server-side cursor in a single read-only
`REPEATABLE READ` transaction, so concurrent inserts/deletes cannot duplicate or omit rows. It fetches
at most 5000 rows into memory per batch (`--limit` changes that batch size, max 5000). `ORDER BY` is
optional for completeness; include it when deterministic output order matters. Ordered columns plus array rows preserve
duplicate column names, UUIDs, numerics, timestamps, and binary values losslessly. Dates render as
`YYYY-MM-DD`, intervals as PostgreSQL text, and `bytea` as base64. Explicit formats are `json`, `ndjson`,
`csv`, and `tsv`:

This snapshot-safe stream replaces the offset-paged `--all` behavior from `rp` 1.19.3 and earlier.

```bash
rp dev console database query prod @report.sql --all --format csv --out ./report.csv
printf 'SELECT id, amount FROM invoices WHERE state = @state' |
  rp dev console database query prod - --param state=open --all --format json --out - |
  jq '.rows[]'
```

#### Binding a console call to a principal

By default a console primitive sees what your login sees. `--principal-kind <kind> --principal-id
<external-id>` (both or neither, on `bash run`, `database query` and `database schema`) binds the call to
an end user instead, so the workspace/query sees exactly the rows a real run for that identity would see.
The response echoes the bound `principal` and lists `hidden_tables` — tables that identity may not read,
which is why an empty result can mean "hidden", not "absent". Both are printed above the result and ride
through `-o json`. Older hosts that do not know the field are unaffected: an unbound call sends no
`principal` at all.

```bash
rp dev console database query prod 'select id from invoices' \
  --principal-kind kampadmin_admin --principal-id 3f1c…-…-…
```

`rp dev console paths [--tenants a,b|all] <path>...|-` reports `dir`/`file`/`missing` for absolute
workspace paths (`/brain`, `/tenant`, `/mirrors/<repo>`, `/kb/<provider>`), one parallel `bash run` per
tenant (bounded by `--concurrency`), chunked under the server's command/output caps. Existence only;
`/skills` is not mounted in the console. Exit 4 when a path is missing or a tenant check failed; a failed
check is that tenant's `error`, never a silent `missing`. Lints should use `-o json --raw-output`.

SQL and bash commands accept a literal argument, `-` for stdin, or `@file`. Console SQL uses `@key`
placeholders; repeated `--param k=v` values are bound server-side as text and never interpolated. The
brain runtime's in-process `lib.db.query` API instead uses its driver's `%s` placeholders. `--limit >500`
requires `--all`. Multi-statement SQL and cross-database queries remain unsupported.

`--out <path>` atomically writes data there and prints a JSON manifest. `--out -` writes data directly
to stdout. `--out auto` writes a unique `.replypen/output/<command>-<runid8>.<ext>` artifact, so
concurrent calls do not clobber each other.

Bash stdout is capped server-side at ~64 KiB. A truncated `bash run` keeps the full stdout in the sandbox
(`stdout_path`) and ends its output (table line, stderr under `-o json`, spill-manifest hint) with the
exact download command; `bash run --out FILE` fetches the full stdout automatically. `file get` copies any
sandbox file or directory without the cap (512 MiB max): directories are tar'ed in the sandbox and
unpacked locally, and paths outside `/tmp`/`/home/agent` are staged through `/tmp` transparently. The
sandbox lives with the warm console session (~2 min idle), so fetch promptly.

```bash
rp dev console file get /tmp/replypen-out/report-abc123.csv ./report.csv
rp dev console file get /kb/knowledgeowl ./kb          # directory → ./kb/
```

Stable process exits: `0` success, `1` usage, `2` auth/scope, `3` truncated, `4` remote bash
non-zero/timeout, `5` server/network or incomplete stream. File outputs are installed atomically only
after the stream's final row-count frame matches the received rows. In JSON/auto-piped mode, command errors use
`{"error":{"code","message","status","fields"}}` on stdout. A bash result is emitted before exit 4
so its `exit_code`, `timed_out`, stdout, and stderr remain inspectable.

Compatibility note for scripts upgrading from rp 1.18: query spills now use unique
`.replypen/output/console-db-query-<run8>.*` paths rather than the old fixed
`.replypen/output/console-db-query/response.json`, and a result above the inline 500-row cap exits 3
instead of succeeding with partial data.

### `rp project review` — the Review page from a script

Same cards, gates and writes as the Review page (`?audience=technical` needs a global admin). An agent
that already fixed a card's issue elsewhere retires it in one step, so the review-implement run never
starts a duplicate worker:

```sh
rp --project dentai project review close 75d0a3dd-d39c-40a2-af97-ae46fed56a3d --note "Fixed in brain 98d13c0, live 2026-10-19"
rp --project dentai project review decide <card-id> --label "Do it" [--instruction "…"]
```

`close` records "Already handled" plus a done receipt holding the note (repeat-safe); `show`/`decide`/
`close` try the owner audience, then technical, unless `--audience` is given.

### `rp project env` — self-serve grounding-env sync

A project's grounding scripts read their credentials (PG DSN, Stripe key, …) from a gitignored `.env`
in the brain clone. `rp project env` lets a project admin or developer with secrets access sync that
**production** env to their laptop over OAuth — the self-serve equivalent of the operator-only
`scripts/rc_env.py --pull` (which needs AWS/SSM access). Run it **from inside the brain clone** (it
reads/writes `./.env`):

```bash
rp project env keys                 # what keys exist (names only — safe to paste/log)
rp project env keys --tenant acme   # the tenant's OWN keys (what set/rm --tenant manage); --effective = merged project + tenant
rp project env pull                 # write ./.env at 0600 — then `brain-dev --live` can run grounding locally
rp project env diff                 # has my local ./.env drifted from production? (names-only; exit≠0 on drift)
printf %s "$SECRET_VALUE" | rp project env set key=FOO_API_TOKEN
rp project env rm FOO_API_TOKEN
rp project env reveal FOO_API_TOKEN # prints the value once; sensitive
```

> **Secret hygiene:** `keys`/`diff` are names-only; `pull` writes values only to the 0600 file; `set`
> reads from STDIN by default and never echoes. `reveal` is the deliberate exception: it prints one live
> secret value for copy/pipe use and audits the key name. The pulled `.env` holds **real production
> secrets** on your laptop — treat it like a password file (it's gitignored in every brain repo). A
> tenant-enabled bulk pull uses the tenant bound to your `rp auth login`, or requires `--tenant` when the
> login is project-pinned. `pull`/`diff` (and `keys --effective`) see the **merged** project + tenant env a
> run sees; plain `keys` with a tenant scope lists only the tenant's own keys.

**Adding a database:** there is no `rp project database add` — a database exists because a sealed
grounding key named `<PROJECT>_<DBKEY>_DSN` exists. `printf %s "$DSN" | rp project env set
key=PROBACKUP_BACKUPS_DSN` registers the DB the brain reaches as `lib.db(db="backups")` (use a read-only
role; the ReplyPen box must be network-allowlisted). Confirm it resolves with `rp dev console database
list`, then annotate it (`rp project database set PROBACKUP_BACKUPS_DSN description=…`) so it appears in
`rp project database ls`. `*_WRITE_DSN` keys belong on `--plane action`, not the grounding plane.

`rp project env set/rm/reveal` targets the grounding plane by default (`/api/v1/env_grounding`), which is
injected into normal read-only runs. Per-key commands target a tenant env only when the OAuth token
itself is tenant-bound; `--tenant` does not retarget `set/rm/reveal`. `--plane action` targets
`.env.action` (`/api/v1/env_action`), the operator-only write-plane for hosted actions; it is
project-level and never enters normal runs.

## Releasing

Use the script — it does the whole cycle reliably and verifies each part:

```bash
scripts/release.sh patch     # 0.1.0 -> 0.1.1  (also: minor | major | vX.Y.Z | --dry-run)
```

A release is **six things that must land together**, which is why a bare `git tag` isn't enough:

1. the exact tested commit pushed and verified on **`origin/main`**;
2. the **git tag** `vX.Y.Z` at that exact commit;
3. the **GitHub Release** + prebuilt binaries — the [release workflow](.github/workflows/release.yml)
   builds every OS/arch via [GoReleaser](https://goreleaser.com) and attaches archives + checksums;
4. the **Go module proxy** ingesting the tag, so consumers' `go get …@latest` resolves the new version
   instead of a stale pseudo-version (the step that's easy to forget by hand).
5. the **Homebrew cask** at the same version;
6. tagged + no-cache-latest copies of `scripts/cloud-setup.sh` in the public release mirror.

The script gates on `go build/vet/test`, refuses a dirty/behind/diverged checkout, then explicitly
pushes the tested SHA to `origin/main` and verifies the remote ref before it creates the tag. It waits
for binaries and warms the proxy afterward. Local `main` may be ahead of origin; publishing it is part
of the release. See [`.claude/skills/release/SKILL.md`](.claude/skills/release/SKILL.md) for the full
runbook and manual fallback.

**Homebrew** is wired up: each release, GoReleaser commits an updated `Casks/rp.rb` **cask** to the
public [`replypen/homebrew-tap`](https://github.com/replypen/homebrew-tap) repo (the
`homebrew_casks:` block in [`.goreleaser.yaml`](.goreleaser.yaml)). It authenticates with the
`HOMEBREW_TAP_GITHUB_TOKEN` repo secret — a token with `contents:write` on the tap repo, since the
default `GITHUB_TOKEN` is scoped to this repo only and can't push to a second one.

It's a **cask** (prebuilt binary), not a source **formula**, on purpose: a non-bottled formula installs
through a Homebrew sandbox + Ruby PTY that fails on some macOS setups (`can't get Master/Slave device`);
a cask just links the binary. A cask is macOS-only and can't share the name `rp` with a formula (bare
`brew install …/rp` would pick the formula — the broken path), so the old `Formula/rc.rb` was deleted
from the tap and a `tap_migrations.json` (`{"rc":"rc"}`) added there so existing installs migrate
formula→cask on `brew upgrade`. The cask was renamed `rc` → `rp`: the tap's `cask_renames.json`
(`{"rc":"rp"}`) makes `brew upgrade` migrate an installed `rc` cask, and the `rp` cask links both `rp`
and `rc`. Linux/WSL/Windows install via [`scripts/install.sh`](scripts/install.sh)
and [`scripts/install.ps1`](scripts/install.ps1), which need no Homebrew.

See [`SKILL.md`](SKILL.md) for the architecture and how to add a command.

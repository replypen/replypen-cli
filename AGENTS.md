# AGENTS.md — replypen-cli (`rp`)

Start here, then open the doc the task needs. This file is a router, not a manual — the detail lives in
the two docs below and in the code.

## What this is (one line)
`rp` is a **scriptable Go client**: it talks to ReplyPen's public `/api/v1` authed with an **OAuth
access token** (`rp auth login`; refreshed transparently), rendered as a table on a TTY or **JSON when piped**.
**Fat client, thin server:** endpoints stay simple and return **raw, token-scoped data**; presentation
and analysis logic (digests, clustering, health roll-ups, diagnosis) is allowed to live **in the CLI**.
Every such command MUST still expose the raw rows via `-o json`, so a consumer can skip our rendering and
do their own thing. No DB access in the CLI — data comes only through `/api/v1`.

## Domain taxonomy

The ReplyPen host (formerly codenamed rootcause) owns the shared
[taxonomy](https://github.com/replypen/replypen/blob/main/AGENTS.md#taxonomy):
project, run, session, thread, principal, brain, mirror, action, and Embassy keep those meanings here.
CLI-specific distinctions:

| Term | Meaning / distinction |
|---|---|
| **Profile** | Local token-store key selecting credentials; `--project` selects server-side scope, not a profile. See [scope model](SKILL.md#scope-model-fail-closed). |
| **Tenant** | Scope below a project; ambient login/brain context can select it, but tenant-record commands use an explicit positional slug. See [scope model](SKILL.md#scope-model-fail-closed). |
| **Scope selector** (`--scope`) | Chooses project or tenant request routing; never grants authorization beyond the token. See [scope contract](internal/cli/scope.go). |
| **Brain marker** | Committed `.replypen.toml` binding a checkout to a project; `.replypen/local.toml` supplies a local tenant override. Neither stores credentials. See [config precedence](SKILL.md#config-precedence). |
| **Brain channel** | Managed project-brain ref (`stable` / `edge`) promoted to an exact commit; distinct from an inbox/chat channel. Tenant overlays use `main`, without channels. See [brain commands](internal/cli/brain.go). |
| **Fleet** | Aggregate views over runs/actions; `--all` fans out across authorized projects, while an ordinary fleet command can remain project-scoped. See [fleet commands](internal/cli/fleet.go). |
| **Learning / reviewed** | Separate run filters: learning selects training signal and excludes held-out threads; reviewed selects human-scored runs, including held-out evaluations. See [run ladder](SKILL.md#the-ladder--index--one-run--detail). |
| **Share token** | Credential carried by a run/chat share link; bypasses profile login and belongs to the link's origin. See [share path](internal/cli/share.go). |
| **Output spill / manifest** | Large payload saved locally; stdout identifies the artifact and may preview it. JSON mode can therefore return a manifest rather than inline rows. See [output contract](docs/specs/progressive-output-disclosure.md). |

## Where to read (pick by task)
- **Running/scripting `rp`, install, a command's flags, releasing** → **[README.md](README.md)** (the user manual; command inventory is generated from Cobra).
- **Changing code** — a command, the HTTP client, OAuth/token/config resolution, render, or output spill → **[SKILL.md](SKILL.md)** first (architecture & intent: the API ladder, the four thin layers, config/auth precedence, the full "adding a command" recipe).
- **Cutting a release** → run `scripts/release.sh`, or read **[.claude/skills/release/SKILL.md](.claude/skills/release/SKILL.md)** for the runbook.
- **A feature's design** → **[docs/specs/](docs/specs/)** (`brain-test-runs.md`, `progressive-output-disclosure.md`).

## Code map (detail in SKILL.md)
- `cmd/rp/main.go` — entrypoint → `cli.Execute(version)`.
- `internal/cli/` — `surface.go` owns the nine roots; command files implement their grouped endpoint adapters. `tokensource.go` is the live token source; `errors.go` surfaces API errors verbatim.
- `internal/client/` — the one HTTP wrapper (`transport.go`, the single send loop: timeout + safe 429/5xx backoff + refresh-on-401, buffered or streamed) + `TokenSource` (`auth.go`) + wire contract (`*_types.go` per endpoint, shared primitives in `types.go`; field names match the server exactly) + `APIError`.
- `internal/digest/` — client-side analysis over raw wire rows (the fat-client half): grounding-source triage order + drift/attention counts, tenant-settings drift, branch-selector heuristics, the chat-doctor diagnosis (`ChatDoctor(inputs, now)`: the CLI does the HTTP/DNS/probe I/O, digest decides findings + verdict). Pure functions over `internal/client` types, no I/O, no clock (depguard/forbidigo-enforced); used by cli + render + debugdump.
- `internal/oauth/` — OAuth protocol client: PKCE loopback + device grant + refresh/revoke (first-party client `rcocl_cli`).
- `internal/token/` — token store `~/.config/replypen/tokens.json` (0600), per-profile.
- `internal/config/` — env-or-production URL resolution + brain-aware profile/project/tenant context (`.replypen.toml` + `.replypen/local.toml`).
- `internal/debugdump/` — the `rp run debug <id>` decomposer (JSONL + thin markdown index).
- `internal/outputspill/` — progressive output disclosure: large stdout/JSON/JSONL spills to `.replypen/output/` (`--out-dir`/`RC_OUTPUT_DIR`), stdout gets a preview/manifest; `--no-preview`/`--raw-output` tune it.
- `internal/render/` — TTY-detect + JSON passthrough (`render.go`) + one file per view family (`runs.go`, `run.go`, `ask.go`, `fleet.go`, `console.go`, …) over the shared cell formatters in `format.go`.
- `internal/dnsdetect/` + `internal/idutil/` — local, offline helpers behind `rp dev tools provider|id`.

## Cloud setup
`scripts/cloud-setup.sh` is the canonical hosted cloud bootstrap. Each release publishes immutable
`<mirror>/<tag>/cloud-setup.sh` plus no-cache `<mirror>/cloud-setup.sh`; users run
`curl -fsSL https://app.replypen.com/install/cloud.sh | bash`. It always installs `rp`; uv/pnpm use a
minimum-version policy (`UV_MIN`/`PNPM_MIN`) that keeps a good-enough preinstalled build and otherwise
installs the pinned one over the shadowing binary. `RC_CLOUD_SKIP_UV=1` / `RC_CLOUD_SKIP_PNPM=1` opt out;
`RC_RELEASE_MIRROR` overrides the mirror.

## Working on it
- **Toolchain:** Go 1.25 via `mise` (pinned in `mise.toml`); `cobra`+`pflag`, `BurntSushi/toml`. Run from the repo dir so mise selects go 1.25.
- **Before finishing any change:** `go build ./... && go vet ./... && go test ./...`, and `gofmt -w`.
- **Golden + generated docs:** fixtures `internal/cli/testdata/*.json` → `*.golden`; the README command inventory and `docs/cli-help.txt` are test-guarded. After changing any command/flag/short, regenerate all of it with `go test ./internal/cli -update`; `internal/render` has its own view goldens (`internal/render/testdata/*.golden`, `go test ./internal/render -update`). Fixtures use canned timestamps — never `time.Now`. Cases whose whole body is "stub server → run → assert" live as rows in `internal/cli/table_test.go` (`TestTableGolden`, `TestJSONPassthrough`, `TestTableLine`, `TestRejectsBeforeRequest`) — add a row there, and only write a standalone `func Test…` when the case needs its own setup or extra asserts.
- **Greenfield release bias:** verified + low regression risk ⇒ release immediately, then update local `rp`.
- **Adding a command / the four thin layers / config precedence** → see [SKILL.md](SKILL.md).

## Scope guards (push back if asked to cross them)
No MCP in v1, no direct DB access (data comes via `/api/v1`), no interactive TUI. Client-side
analysis/rendering is fine; keep the server endpoints **thin and raw** (raw rows, not server-computed
views) with `-o json` always exposing them. Auth is **OAuth only** against the server's existing
`/oauth/*` + the first-party client `rcocl_cli` — the CLI invents no auth of its own. Writes go **only**
through endpoints the public `/api/v1` already serves (config/resource CRUD, run triggers/feedback,
brain promote/publish/edit, the guarded `dev console` write plane, `admin`); the CLI adds no new server
endpoint. `rp project env pull` writes a local `./.env` only (a GET) and never prints secret values.

## Legacy names kept (ReplyPen rename; old installs and brains depend on them)
- `rc` command — alias of `rp` in every install path (cask target, `rc → rp` symlink, `rc.exe` copy on Windows); `scanPathRC` treats it as the same install.
- `rc_<ver>_<os>_<arch>` release archives (build `rc-legacy`) — installed `rc` ≤ 1.51 self-update by extracting a file named `rc` from them.
- S3 mirror prefix `cloud-bootstrap/rc` — baked into installed binaries and the host's `/install/cloud.sh` redirect; the IAM role `rootcause-cli-release-mirror` can only write there.
- Module path `github.com/rootcause-org/rootcause-cli` (`/cmd/rc`) — recognised by `isOurBuild` so doctor/`--migrate` know old Go installs.
- `~/.config/rootcause` — left as a symlink to `~/.config/replypen` (`internal/token/migrate.go`): one token store + lock namespace for old and new binaries.
- `.rootcause.toml`, `.rootcause/local.toml`, `ROOTCAUSE_BASE_URL` — still read (`internal/config`); `.rootcause/` is still gitignore-seeded (`internal/localstate`).
- `RC_*` env vars — shared with the workspace runtime contract.
- OAuth client id `rcocl_cli` — persisted server-side identity.
- Homebrew tap's local name `rootcause-org/tap` — survives the GitHub org rename on existing machines (`installedCask`).

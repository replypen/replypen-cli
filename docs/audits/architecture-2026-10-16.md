# Architecture audit — rootcause-cli — 2026-10-16
Intent: `AGENTS.md`, `SKILL.md`, `docs/specs/progressive-output-disclosure.md`, prior `architecture-2026-09-05.md` · Scope: 42 commits since 2026-10-02 (hotspots `debugdump/index.go`, `client/run_types.go`, `cli/{attachments,console_download,console_paths,chat,session_search,token_ls}.go`) + server wire truth in `rootcause/internal/api/prompt.go` · Not audited: nothing skipped (suite offline).

Method: pjarchitecture scripts as pointers → 2 Opus sub-agents (cli · debugdump/client/render) + independent **Astra Medium** opinion (withheld findings) → every item re-opened in source → Opus implementation → **Sol High** review.

> [!NOTE]
> Graph still clean: 0 cycles, 0 layer violations, golangci (depguard/forbidigo) + `lint-contracts.sh` green. 09-05 landings held; no regression of F2/F5/F9–F15.

## Shipped

| # | defect (evidence) | fix · proof |
|---|---|---|
| 1 | Run decline read from wire fields the server never sends (`debug.decline_reason`, `source_pr`, trace `draft_markdown`/`decline_reason`; server `prompt.go:666,1120`). Declined runs showed no reason in `run trace`/`run debug`; fixtures invented the shape | real fields (`decline_reason` on `/runs/{id}`, `decline` on `/trace`); phantom fields, `RunDebug`, `SourcePR` + render paths deleted; index "Declined" block, JSONL header `decline` · `TestDeclineRendersInDebugDump` |
| 2 | gzip CRC/size never checked: `extractTarGz` drained the raw reader at tar EOF (`console_download.go:239`) → corrupt dir installed as complete | drain through `gz` · `TestExtractTarGzRejectsCorruptTrailer` (failed before) |
| 3 | Debug flags + "Files read" blind after the 14-day scrub (`analysis.go` used empty `e.command`) | fall back to redacted `kept.Command`; repeats stay raw · `TestScrubbedGrepMissUsesKeptCommand` |
| 4 | "no stored callback" defined twice, drifted (`analysis.go:47` vs `index.go:671`) | one `hasCallback` · `TestAnswerOnlyRunIsNotFlaggedAsNoCallback` |
| 5 | chat doctor: map-order findings; decode errors swallowed → false all-clear exit 0 | sorted; decode error returned · 2 tests |
| 6 | Retry-After unbounded (3 retries × server value) | capped 30 s · `TestRetryDelayCapsRetryAfter` |
| 7 | Server session id used as local folder name unchecked (`attachments.go:208`) | UUID check before mkdir · test |
| 8 | `TestRejectsBeforeRequest` never asserted "before request" | request counter (scope-resolution whoami/projects allowed); 4 server-data rows → `TestRejectsWithServerData`; session-search rejects folded in |
| 9 | Suite sinks: refresh-race test slept 2 s to infer parallelism; session-search 500 waited real backoff (1.75 s) | barrier (mutation-checked: store-wide lock fails it) + `Retry-After: 0` · cli pkg **8.4 s → 4.9 s** |
| 10 | SKILL.md claimed 2 JSON exceptions; reality has more | documented console `hint`, token-ls envelope, mirror-refresh `git fetch` |

Released **v1.51.1**; live-verified on a declined DA run (`run show` Why, `run trace` Decline block, `run debug` index + JSONL `decline` — all empty before). Sol High review: 1 finding (scrubbed loud failure read as grep miss) fixed + mutation-checked.

Suite: `go test ./... -count=1` 8.75 s wall (was 10.0 s); cli 4.9 s, releasetest 2.1 s (skipped by `-short`).

## Needs PJ's green light (ranked)

### P1. Decide the JSON contract for computed commands
Evidence: doctors invent envelopes (`chat.go:568`, `action_dx.go`), `console_paths.go:96` emits a computed report and drops raw bash bodies, `withJSONHint` splices a key into a server body (`console_download.go:323`), `token_ls.go:49` filters. AGENTS.md says raw rows MUST stay reachable; SKILL.md says JSON can't invent a field. Both models flagged it independently.
Proposal: rule = "passthrough commands: verbatim; computed reports: `{report…, raw:{<endpoint>: body}}`; CLI annotations to stderr/manifest, never inside a server body". Benefit: consumers can trust `-o json`; unblocks P2. Effort S (doc) + M (≈5 commands). Risk: breaks scripts reading today's shapes — why approval.

### P2. Chat doctor → pure `digest.ChatDoctor(inputs, now)` + `render.ChatDoctor` (old F6, grew 143 → 175 LOC, does DNS lookups in `RunE`)
Benefit: goldens possible, diagnosis testable without stubs. Effort M (~250 LOC moved), blast radius 2 files. Depends on P1.

### P3. Route all `-o json` through the spill (`e.renderJSON`)
98 direct `render.JSON(e.out,…)` vs 53 spill-aware; new big outputs (`run feedback list`, `dev replies`, `chat replay`, attachments) can flood an agent context. Spec lists a coverage set, so this widens the contract. Effort S (mechanical + forbidigo rule with secret-reveal exemptions). Risk: low, but changes stdout for large payloads fleet-wide.

### P4. `run debug` JSONL header: pass the raw run object instead of an allowlist (`jsonl.go:22-48`)
`jsonl.go` changed in 6 feature commits in two weeks, each to plumb one new server field each; `questions`, `answers`, `parent_run_id`, `guards`, `prior_*` never reach the JSONL. Effort S–M. Risk: JSONL is consumed by the Python operator script — key-set change needs a compat check.

<details><summary>Lower value, not proposed now</summary>

- `ask --replay-chat-run` repeats the submit/wait tail and fetches `/runs/{id}` twice (`ask.go:218-290`, `:490`) — S, do when next touched.
- Health verdict decided twice in `render/health.go:33-61` vs `:96-101` → `digest.HealthRollup` — S, exit code could drift from table.
- Formatter re-duplication (`humanBytes` ×2 with different output, `truncateRunes`, `dashCLI`, `short`, `containsCLI`) — XS each; old F8 pattern returning.
- Extract size cap in `extractTarGz` — own sandbox → low threat; skipped.
- Still open from 09-05: F13 (KB Python in Go strings, needs server endpoint), L7 (`RC_HTTP_TIMEOUT` read in `client`).
</details>

## Second-opinion synthesis
Astra (independent) found #2, the barrier fix (#9) and P1; Opus sub-agents found #1, #3–#8, P2–P4. Agreement: no file splits for size, no transport rewrite, no test cuts without survivors. Astra's "document existing JSON exceptions now, approve shape changes" taken as-is (#10 + P1).

## Deliberately not done
- `index.go` (852 LOC) section-table refactor: features already land as `renderX` + one `add()` line; a registry adds indirection without removing conflicts. Extract the inline header/timeline blocks when next touched.
- No lint rule added: the useful one (`render.JSON` in cli) only makes sense after P3.

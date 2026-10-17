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

## Round 2 — P1–P4 (approved 2026-10-17)

Two independent designs (Opus + Astra) → Opus implementation → Sol High review: approve, 0 findings.

| P | commit | what landed |
|---|---|---|
| P1 | `a2ad50f` | SKILL.md rule: passthrough verbatim (row filters ok) · computed reports keep keys + top-level `raw:{source: body}` · CLI notes never inside a server body. chat/action doctor `-o json` carry `raw`; `--bundle` keeps lean shape, no raw; bash-run `hint` → stderr + manifest |
| P2 | `1b48b0c` | `digest.ChatDoctor(inputs, now)` + `render.ChatDoctor` + golden; CLI keeps I/O. JSON byte-identical over 18 combos except `raw`. Also fixed forbidigo `time.Now`/`os.Getenv` bans that never fired |
| P3 | `db8924a` | `renderJSONInline` (93 sites, no behaviour change) vs spill-aware `renderJSON`; 4 new outputs spill (`run feedback list`, `dev replies list`, `project chat replay`, `run attachments --download`); forbidigo bans bare `render.JSON` in cli |
| P4 | `ba8d1a7` | JSONL header = server raw `run` + CLI overlay (owned keys win, host-only keys dropped, event-key collisions → `run_*`) |

> [!IMPORTANT]
> **P3 deviates from the approved wording on purpose.** "Spill everything" would hand ≥16 sibling scripts (brain-skills `context.py`, `simulate.py`, `brain_projection.py`, `sent_delta_report.py`, `brain_git_sync.py`, DentAdmin `run_eval.py`, PB `autoqa/_rc.py`) a manifest instead of JSON above 6 KB. Both planners rejected it. Follow-up: migrate those scripts to `--raw-output`, then flip commands to spill one by one.

Live (local build vs prod, DA run `2d08cf43`): JSONL header gains `guards`, `scenario`, no key lost; chat doctor `raw` has 5 sources, `--bundle` none. Bash-run hint move: tests only, not live.

> [!NOTE]
> Shipped in **v1.53.0** (the [ReplyPen rename](t3-thread://v1/mcp:9d03e3c2-c948-4198-8a94-a65a9422f0b3) moved the repo mid-release; v1.52.0 never published, its code is in 1.53.0). Re-verified live on `rp` 1.53.0: `run show/trace/debug` decline, JSONL `guards`/`scenario`, doctor `raw` (5 sources) vs `--bundle` (none), same exit code table/json, `run feedback list` spills at a low threshold while script-consumed `project repo ls` stays inline.

Existing risk found (not introduced): `rc project tenant profile get` already spills; `brain_projection.py` parses it without `--raw-output` → breaks on profiles > 6 KB fields / 20 KB total. Fix belongs in brain-skills (`--raw-output`).

## Lower value, not proposed

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

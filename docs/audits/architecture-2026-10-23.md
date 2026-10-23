# Architecture audit — replypen-cli (rp) — 2026-10-23
Window: 39 commits since 10-16 (platform-skills pull, triage eval, knowledge sync, publish, `--credential-env`, short ids). Previous: [`architecture-2026-10-16.md`](architecture-2026-10-16.md).
Method: Opus auditor (2 sub-agents) + independent Astra → lead re-verified → Opus builder → Sol High ("ship with minor"; minor fixed by lead).

## Shipped — released v1.69.0
| Fix | Bug it removes |
|---|---|
| Platform-skill extract drains gzip and checks the trailer | corrupt archive installed (the console extractor already had this check) |
| Manifest digest validated before use as a path; rename race = success | empty/short digest → cache root "verified" or panic; concurrent pulls failed |
| `normalizeRunID` on `run feedback/retry`/phone-lab; `cardIDArg` strips any `<kind>_<uuid>` | documented `#96745381` / `ac_…` ids 404'd |
| Triage eval classifies on judged attempts; zero attempts = failed_open | one fail-open attempt hid a false skip; empty attempts counted as pass |
| Legacy-submit fallback refuses when credentials are set | `--credential-env` silently dropped on old hosts |
| `raw` on tenant-brain/mirror/channel publish JSON | unmodelled server fields dropped (rule b) |

## Test suite speed
`go test ./... -p 4` 8 s, 1 074 tests. No cuts.

## Guardrails
lint-contracts ok, golangci 0, gofmt clean. `RULES=all` 1 known hit (`kb_content.go:792`, F13).

## Deliberately not done
Formatter dupes (XS), digest test vector shared with replypen, file splits.

package debugdump

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/rootcause-org/rootcause-cli/internal/client"
	"github.com/rootcause-org/rootcause-cli/internal/digest"
)

// EmitJSONL writes the drill-down event log: a {"type":"run"} header line (run metadata + full
// draft/note bodies + the untrimmed system prompt + egress) followed by one {"type":"event"} line per
// tool call, every field FULL and untruncated, keyed by `disp`. Header rollups are `run_`-prefixed so
// event-space jq queries (`select(.duration_ms > 60000)`) never match the header.
//
// raw is the verbatim /trace body (FullWithRaw / SharedRunTrace): header = the server's raw `run`
// object + the CLI overlay (runHeader), so a server field the CLI does not model yet still reaches jq.
// nil (or an undecodable body) degrades to the overlay alone — the pre-raw allowlist.
func EmitJSONL(w io.Writer, full *client.FullResponse, raw json.RawMessage) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(runHeader(full, raw)); err != nil {
		return err
	}
	return emitEvents(enc, decorate(full.Events))
}

// runHeader is the {"type":"run"} line: the server's raw `run` object as the base, then the overlay on
// top. The small compat layer between the two:
//   - every key the overlay OWNS (overlayKeys, emitted or not) is deleted from the base first, so each
//     existing key keeps exactly its pre-raw value and semantics — emptyNil nulls, parsed tenant_settings,
//     the notes/egress shapes, the host-only-filtered metadata — and a conditional overlay key the overlay
//     omits (empty mirror_refs, a non-redacted detail_redacted) stays omitted rather than reappearing raw;
//   - a host-only top-level key (client.HostOnlyMetadataKey: spend/tokens/serving model, which an older
//     server emits on the run itself) is dropped, as metadataJSON already does one level down;
//   - a raw key that is also an EVENT key (eventKeys) is renamed `run_<key>` (dropped if that name is
//     taken), so event-space jq never matches the header. Today only `status` collides, and it is owned.
//
// Numbers and nested objects stay json.RawMessage bytes: never float-decoded, unknown fields verbatim.
func runHeader(full *client.FullResponse, raw json.RawMessage) map[string]any {
	base := rawRunObject(raw)
	header := make(map[string]any, len(base)+len(overlayKeys))
	for k, v := range base {
		if overlayKeys[k] || client.HostOnlyMetadataKey(k) {
			continue
		}
		if eventKeys[k] {
			if _, taken := base["run_"+k]; taken || overlayKeys["run_"+k] {
				continue
			}
			k = "run_" + k
		}
		header[k] = v
	}
	for k, v := range overlayHeader(full) {
		header[k] = v
	}
	return header
}

// rawRunObject decodes `.run` of the verbatim /trace body into per-key raw bytes; nil when there is no
// usable body (old callers, a non-object run), which leaves the overlay on its own.
func rawRunObject(raw json.RawMessage) map[string]json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	var bundle struct {
		Run map[string]json.RawMessage `json:"run"`
	}
	if json.Unmarshal(raw, &bundle) != nil {
		return nil
	}
	return bundle.Run
}

// overlayKeys is every key overlayHeader can emit. Keep it in lockstep with overlayHeader
// (TestOverlayKeysCoverOverlay): a key missing here would let the raw base reintroduce a field the
// overlay deliberately omits or reshapes.
var overlayKeys = map[string]bool{
	"type": true, "run_id": true, "project": true, "tenant": true, "status": true, "kind": true,
	"trigger": true, "brain_ref": true, "brain_resolved": true, "tenant_settings": true,
	"tenant_settings_current": true, "error": true, "thread_id": true, "session_id": true, "topic": true,
	"question": true, "warm_start_digest": true, "grounding_seed": true, "system_prompt": true,
	"created_at": true, "finished_at": true, "draft": true, "decline": true, "notes": true, "metadata": true,
	"egress": true, "detail_redacted": true, "grounding_sources": true, "grounding_source_drift_count": true,
	"mirror_refs": true, "answer_markdown": true, "replay_of_run_id": true, "inspiration": true,
	"proposed_actions": true, "outbound_email": true, "reply_scope": true, "context_schema_version": true,
	"prompt_sections": true, "manifest_blocks": true, "bootstrap_turn": true, "context_capture_origin": true,
	"context_capture_origin_run_id": true, "context_captured_at": true, "prompt_decomposition": true,
	"preselected_turn": true, "tenant_settings_drift": true,
}

// eventKeys is every key an event line can carry (emitEvents; TestEventKeysCoverEvents). A raw header
// key in this set is renamed so it can't satisfy an event-space jq filter.
var eventKeys = map[string]bool{
	"type": true, "disp": true, "seq": true, "grounding": true, "tool": true, "label": true, "command": true,
	"stdout": true, "stderr": true, "exit_code": true, "status": true, "duration_ms": true, "at": true,
	"reasoning": true, "args": true, "output_scrubbed_at": true, "output_bytes": true, "output_sha256": true,
	"kept": true,
}

// overlayHeader is the CLI-computed header: the pre-raw allowlist, unchanged. It owns every key in
// overlayKeys; runHeader layers it over the server's raw run object.
func overlayHeader(full *client.FullResponse) map[string]any {
	r := full.Run
	header := map[string]any{
		"type":                    "run",
		"run_id":                  r.RunID,
		"project":                 r.Project,
		"tenant":                  emptyNil(r.Tenant),
		"status":                  r.Status,
		"kind":                    r.Kind,
		"trigger":                 emptyNil(r.Trigger),
		"brain_ref":               emptyNil(r.BrainRef),
		"brain_resolved":          emptyNil(r.BrainResolved),
		"tenant_settings":         tenantSettingsJSON(r.TenantSettings),
		"tenant_settings_current": tenantSettingsJSON(r.TenantSettingsCurrent),
		"error":                   emptyNil(r.Error),
		"thread_id":               emptyNil(r.ThreadID),
		"session_id":              emptyNil(r.SessionID),
		"topic":                   emptyNil(r.Topic),
		"question":                emptyNil(r.Question),
		"warm_start_digest":       emptyNil(r.WarmStartDigest),
		"grounding_seed":          emptyNil(r.GroundingSeed),
		"system_prompt":           emptyNil(r.SystemPrompt),
		"created_at":              emptyNil(r.CreatedAt),
		"finished_at":             emptyNil(r.FinishedAt),
		"draft":                   emptyNil(r.Draft),
		"decline":                 emptyNil(r.Decline),
		"notes":                   notesJSON(r.Notes),
		"metadata":                metadataJSON(r.Metadata),
		"egress":                  egressJSON(r.Egress),
	}
	// A redacted bundle still gets its JSONL (whatever the server sent) — but the header must carry the
	// marker so a jq consumer can tell "nothing happened" from "nothing was served".
	if full.Redacted() {
		header["detail_redacted"] = true
	}
	if len(r.GroundingSourcesRaw) > 0 {
		header["grounding_sources"] = json.RawMessage(r.GroundingSourcesRaw)
		header["grounding_source_drift_count"] = digest.GroundingSourceDriftCount(r.GroundingSources)
	} else if r.GroundingSources != nil {
		header["grounding_sources"] = r.GroundingSources
		header["grounding_source_drift_count"] = digest.GroundingSourceDriftCount(r.GroundingSources)
	}
	if len(r.MirrorRefs) > 0 {
		header["mirror_refs"] = r.MirrorRefs
	}
	// A raw or chat-replay run's reply is its answer, not a draft.
	if r.AnswerMarkdown != "" {
		header["answer_markdown"] = r.AnswerMarkdown
	}
	if r.ReplayOfRunID != "" {
		header["replay_of_run_id"] = r.ReplayOfRunID
	}
	if len(r.Inspiration) > 0 {
		header["inspiration"] = json.RawMessage(r.Inspiration)
	}
	if len(r.ProposedActions) > 0 {
		header["proposed_actions"] = r.ProposedActions
	}
	// WHO the draft goes to: absent on an ordinary reply-to-sender, present when the run chose explicit
	// recipients (reply.outbound_email) — the body alone never reveals that.
	if r.OutboundEmail != nil {
		header["outbound_email"] = r.OutboundEmail
	}
	if r.ReplyScope != "" {
		header["reply_scope"] = r.ReplyScope
	}
	// The persisted prompt context, under the server's own field names so the rc-debug jq recipes read
	// the same keys the API documents. context_schema_version is written even when 0: that zero IS the
	// "this run predates the capture / aged out" signal, and a jq consumer needs it present to test it.
	header["context_schema_version"] = r.ContextSchemaVersion
	if len(r.PromptSections) > 0 {
		header["prompt_sections"] = json.RawMessage(r.PromptSections)
	}
	if len(r.ManifestBlocks) > 0 {
		header["manifest_blocks"] = json.RawMessage(r.ManifestBlocks)
	}
	if r.BootstrapTurn != "" {
		header["bootstrap_turn"] = r.BootstrapTurn
	}
	for key, v := range map[string]string{
		"context_capture_origin":        r.ContextCaptureOrigin,
		"context_capture_origin_run_id": r.ContextCaptureOriginRunID,
		"context_captured_at":           r.ContextCapturedAt,
		"prompt_decomposition":          r.PromptDecomposition,
	} {
		if v != "" {
			header[key] = v
		}
	}
	if r.PreselectedTurn != "" {
		header["preselected_turn"] = r.PreselectedTurn
	}
	if drift, err := digest.TenantSettingsDrift(r.TenantSettings, r.TenantSettingsCurrent); err == nil && len(drift) > 0 {
		header["tenant_settings_drift"] = drift
	}
	return header
}

func emitEvents(enc *json.Encoder, events []decEvent) error {
	for _, e := range events {
		line := map[string]any{
			"type":        "event",
			"disp":        e.disp,
			"seq":         e.src.Seq,
			"grounding":   e.grounding,
			"tool":        e.src.Tool,
			"label":       e.label,
			"command":     e.command,
			"stdout":      emptyNil(e.src.Stdout),
			"stderr":      emptyNil(e.src.Stderr),
			"exit_code":   e.src.ExitCode,
			"status":      e.src.Status,
			"duration_ms": e.src.DurationMs,
			"at":          emptyNil(e.src.At),
			"reasoning":   emptyNil(e.src.Reasoning),
		}
		// Bash's full input is `command`; other tools carry their structured input in `args`.
		if e.src.Tool != "bash" {
			if len(e.src.Args) > 0 {
				line["args"] = json.RawMessage(e.src.Args)
			} else {
				line["args"] = map[string]any{}
			}
		}
		if e.src.OutputScrubbedAt != "" {
			line["output_scrubbed_at"] = e.src.OutputScrubbedAt
		}
		if e.src.OutputBytes != 0 {
			line["output_bytes"] = e.src.OutputBytes
		}
		if e.src.OutputSHA256 != "" {
			line["output_sha256"] = e.src.OutputSHA256
		}
		if e.kept != nil {
			line["kept"] = e.src.Kept
		}
		if err := enc.Encode(line); err != nil {
			return err
		}
	}
	return nil
}

// --- JSON shapers (header fields that need a null or typed shape, not a bare string) ------------------

func emptyNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func notesJSON(notes []client.Note) []map[string]any {
	out := make([]map[string]any, 0, len(notes))
	for _, n := range notes {
		out = append(out, map[string]any{"key": n.Key, "body": n.Body})
	}
	return out
}

// metadataJSON passes the run's freeform metadata into the JSONL header, minus the host-only keys an
// older server may still emit (spend/tokens, serving model identity) — the dump is a read surface like
// any other.
func metadataJSON(m map[string]any) any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if client.HostOnlyMetadataKey(k) {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func tenantSettingsJSON(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if json.Valid([]byte(raw)) {
		return json.RawMessage(raw)
	}
	return raw
}

func egressJSON(egress []client.EgressItem) []map[string]any {
	out := make([]map[string]any, 0, len(egress))
	for _, g := range egress {
		out = append(out, map[string]any{"host": g.Host, "count": g.Count, "blocked": g.Blocked})
	}
	return out
}

package debugdump

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/replypen/replypen-cli/internal/client"
)

// headerOf emits a trace (typed decode of raw + raw itself) and returns the JSONL header line's bytes
// plus its per-key raw decode.
func headerOf(t *testing.T, raw string) ([]byte, map[string]json.RawMessage) {
	t.Helper()
	var full client.FullResponse
	if err := json.Unmarshal([]byte(raw), &full); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := EmitJSONL(&buf, &full, json.RawMessage(raw)); err != nil {
		t.Fatalf("EmitJSONL: %v", err)
	}
	line := bytes.SplitN(buf.Bytes(), []byte("\n"), 2)[0]
	var head map[string]json.RawMessage
	if err := json.Unmarshal(line, &head); err != nil {
		t.Fatalf("decode header: %v", err)
	}
	return line, head
}

// The header's base is the server's raw run object: a field the CLI does not model (nested unknowns, a
// >2^53 integer) arrives byte-for-byte, while every key the overlay owns keeps its CLI shape and the
// suppression rules (host-only keys, event-key collisions, omitted conditional keys) still hold.
func TestJSONLHeaderPassesRawRunObject(t *testing.T) {
	line, head := headerOf(t, `{"run":{
		"run_id":"r1","status":"done","kind":"prompt","trigger":"","thread_id":"",
		"parent_run_id":"r0",
		"questions":[{"id":"q1","future":{"deep":[1,{"x":null}]},"big":9007199254740993}],
		"model":"claude-opus-5","cost_usd":0.42,
		"metadata":{"outcome":"ok","model":"claude-opus-5","tokens":12},
		"mirror_refs":{},
		"notes":[{"key":"k","body":"b","extra":"dropped by the overlay shape"}],
		"duration_ms":90000,
		"seq":7,"run_seq":"server-owned"
	},"events":[{"seq":1,"tool":"bash","status":"ok","command":"ls","duration_ms":5}]}`)

	for _, want := range []string{
		`"questions":[{"id":"q1","future":{"deep":[1,{"x":null}]},"big":9007199254740993}]`,
		`"parent_run_id":"r0"`,
		`"run_duration_ms":90000`,
		`"run_seq":"server-owned"`,
		`"status":"done"`,
		`"trigger":null`, `"thread_id":null`,
		`"metadata":{"outcome":"ok"}`,
		`"notes":[{"body":"b","key":"k"}]`,
	} {
		if !bytes.Contains(line, []byte(want)) {
			t.Errorf("header missing %s:\n%s", want, line)
		}
	}
	for _, gone := range []string{"model", "cost_usd", "duration_ms", "seq", "mirror_refs"} {
		if _, ok := head[gone]; ok {
			t.Errorf("header kept %q: %s", gone, head[gone])
		}
	}
}

// A redacted trace still gets the raw base (whatever the server sent) plus the withheld marker.
func TestJSONLHeaderRedactedRaw(t *testing.T) {
	_, head := headerOf(t, `{"run":{"run_id":"r1","status":"done","kind":"prompt","detail_redacted":true,"model":"m","scenario":"s1"},"events":[]}`)
	if string(head["detail_redacted"]) != "true" || string(head["scenario"]) != `"s1"` {
		t.Fatalf("redacted header = %v", head)
	}
	if _, ok := head["model"]; ok {
		t.Fatal("redacted header kept host-only model")
	}
}

// No raw body (an old caller) degrades to exactly the overlay — the pre-raw allowlist.
func TestJSONLHeaderWithoutRawIsOverlay(t *testing.T) {
	full := &client.FullResponse{Run: client.RunHeader{RunID: "r1", Status: "done", Kind: "prompt"}}
	got, err := json.Marshal(runHeader(full, nil))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(overlayHeader(full))
	if !bytes.Equal(got, want) {
		t.Fatalf("nil raw header = %s, want overlay %s", got, want)
	}
}

// overlayKeys must name exactly what overlayHeader can emit: a missing name would let the raw base
// reintroduce a key the overlay omitted or reshaped.
func TestOverlayKeysCoverOverlay(t *testing.T) {
	ts := func(v string) string {
		return `{"source":"cli","version":"v","settings":{"method":"` + v + `"}}`
	}
	full := &client.FullResponse{DetailRedacted: true, Run: client.RunHeader{
		RunID: "r1", Status: "done", Kind: "prompt",
		TenantSettings: ts("a"), TenantSettingsCurrent: ts("b"),
		GroundingSourcesRaw: json.RawMessage(`{"captured":true}`), MirrorRefs: map[string]string{"app": "x"},
		AnswerMarkdown: "a", ReplayOfRunID: "r0", Inspiration: json.RawMessage(`{}`),
		ProposedActions: []client.ProposedAction{{ID: "p"}}, OutboundEmail: &client.OutboundEmail{},
		ReplyScope: "s", PromptSections: json.RawMessage(`[]`), ManifestBlocks: json.RawMessage(`[]`),
		BootstrapTurn: "b", PreselectedTurn: "p", ContextCaptureOrigin: "o", ContextCaptureOriginRunID: "r",
		ContextCapturedAt: "t", PromptDecomposition: "d",
	}}
	got := overlayHeader(full)
	for k := range got {
		if !overlayKeys[k] {
			t.Errorf("overlayHeader emits %q, missing from overlayKeys", k)
		}
	}
	for k := range overlayKeys {
		if _, ok := got[k]; !ok {
			t.Errorf("overlayKeys names %q, overlayHeader never emitted it (fixture incomplete or key stale)", k)
		}
	}
}

// eventKeys must cover every key an event line can carry, or a colliding raw header key would match an
// event-space jq filter.
func TestEventKeysCoverEvents(t *testing.T) {
	events := []client.EventItem{
		{Seq: 1, Tool: "bash", Status: "ok", Command: "ls", Stdout: "o", Stderr: "e", Reasoning: "r", At: "t",
			OutputScrubbedAt: "t", OutputBytes: 1, OutputSHA256: "h", Kept: json.RawMessage(`{"v":1,"command":"ls"}`)},
		{Seq: 2, Tool: "read", Status: "ok", Args: json.RawMessage(`{"path":"/x"}`)},
	}
	var buf bytes.Buffer
	if err := emitEvents(json.NewEncoder(&buf), decorate(events)); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, l := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var ev map[string]json.RawMessage
		if err := json.Unmarshal(l, &ev); err != nil {
			t.Fatal(err)
		}
		for k := range ev {
			seen[k] = true
			if !eventKeys[k] {
				t.Errorf("event line carries %q, missing from eventKeys", k)
			}
		}
	}
	for k := range eventKeys {
		if !seen[k] {
			t.Errorf("eventKeys names %q, no event line emitted it", k)
		}
	}
}

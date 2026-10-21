package debugdump

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPhoneLabJSONLPreservesFutureFieldsAndPositions(t *testing.T) {
	raw := json.RawMessage(`{"run_id":"r","future":{"kept":true},"events":[{"t":20,"kind":"tool","stdout":"full\nbody","future":123},{"t":10,"kind":"model","seq":1,"result":{"tool_calls":[]}}]}`)
	data, err := PhoneLabJSONL(raw)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines=%d", len(lines))
	}
	var header struct {
		Type string                     `json:"type"`
		Run  map[string]json.RawMessage `json:"run"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &header); err != nil {
		t.Fatal(err)
	}
	if header.Type != "phone_run" || string(header.Run["future"]) != `{"kept":true}` || header.Run["events"] != nil {
		t.Fatalf("header: %s", lines[0])
	}
	var event struct {
		Index int                        `json:"index"`
		Event map[string]json.RawMessage `json:"event"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &event); err != nil {
		t.Fatal(err)
	}
	if event.Index != 0 || string(event.Event["future"]) != "123" || string(event.Event["stdout"]) != `"full\nbody"` {
		t.Fatalf("event: %s", lines[1])
	}
	if _, err := PhoneLabJSONL(json.RawMessage(`{"events":{}}`)); err == nil {
		t.Fatal("accepted invalid events")
	}
}

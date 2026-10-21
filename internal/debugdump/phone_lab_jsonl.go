package debugdump

import (
	"bytes"
	"encoding/json"
)

// The envelope leaves API fields untouched, including future event fields and original array positions.
func PhoneLabJSONL(raw json.RawMessage) ([]byte, error) {
	var header map[string]json.RawMessage
	if err := json.Unmarshal(raw, &header); err != nil {
		return nil, err
	}
	var events []json.RawMessage
	if err := json.Unmarshal(header["events"], &events); err != nil {
		return nil, err
	}
	delete(header, "events")
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	if err := enc.Encode(struct {
		Type string                     `json:"type"`
		Run  map[string]json.RawMessage `json:"run"`
	}{"phone_run", header}); err != nil {
		return nil, err
	}
	for i, event := range events {
		if err := enc.Encode(struct {
			Type  string          `json:"type"`
			Index int             `json:"index"`
			Event json.RawMessage `json:"event"`
		}{"event", i, event}); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}

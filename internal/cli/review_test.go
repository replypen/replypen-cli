package cli

import (
	"encoding/json"
	"testing"
)

// finish has no server body to pass through; -o json still answers JSON a script can parse.
func TestReviewFinishJSON(t *testing.T) {
	srv := stubServer(t)
	defer srv.Close()
	e, out, _ := newTestEnv(t, srv, "json")
	if err := run(t, e, "--project", "alpha", "project", "review", "finish", "--audience", "technical"); err != nil {
		t.Fatal(err)
	}
	var got struct {
		SessionID string `json:"session_id"`
		Finished  bool   `json:"finished"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || !got.Finished || got.SessionID != "5e551011-0000-0000-0000-000000000001" {
		t.Fatalf("finish json: %v %q", err, out.String())
	}
}

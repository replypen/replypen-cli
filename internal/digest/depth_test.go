package digest

import (
	"encoding/json"
	"testing"
)

func TestDepthLine(t *testing.T) {
	for _, tc := range []struct{ args, want string }{
		{`{"tier":"pro","source":"router","choice":"substantial","p_simple":0.01,"confidence":0.92,"previous_tier":"","switched":false}`, "depth: pro (router: substantial p=0.01)"},
		{`{"tier":"pro","source":"router","choice":"substantial","p_simple":0.01,"previous_tier":"standard","switched":true}`, "depth: pro (switched from standard; router: substantial p=0.01)"},
		{`{"tier":"pro","source":"router"}`, "depth: pro (router)"},
		{`{"tier":"pro","source":"sticky","previous_tier":"pro"}`, "depth: pro (sticky)"},
		{`{"tier":"standard","source":"user"}`, "depth: standard (user)"},
		{`{"tier":"pro","source":"user","previous_tier":"standard","switched":true}`, "depth: pro (switched from standard; user)"},
	} {
		d, ok := ParseDepth(json.RawMessage(tc.args))
		if !ok || d.Line() != tc.want {
			t.Errorf("%s → %q, want %q", tc.args, d.Line(), tc.want)
		}
	}
	if _, ok := ParseDepth(json.RawMessage(`{}`)); ok {
		t.Error("empty args parsed as a depth decision")
	}
}

func TestLiftDepth(t *testing.T) {
	got := LiftDepth([]string{"a", "b", "depth", "c"}, func(s string) bool { return s == "depth" })
	if len(got) != 4 || got[0] != "depth" || got[1] != "a" || got[3] != "c" {
		t.Fatalf("LiftDepth = %v", got)
	}
}

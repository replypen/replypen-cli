package digest

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/replypen/replypen-cli/internal/client"
)

func TestSearchReportFrom(t *testing.T) {
	b, err := os.ReadFile("../cli/testdata/search_events.json")
	if err != nil {
		t.Fatal(err)
	}
	var l client.SearchEventList
	if err := json.Unmarshal(b, &l); err != nil {
		t.Fatal(err)
	}
	r := SearchReportFrom(&l)
	if r.Searches != 3 || r.Clicks != 1 || len(r.Queries) != 2 {
		t.Fatalf("report = %+v", r)
	}
	q := r.Queries[0]
	// Case-folded into one query; the click (no q) joins sid-a's search; sid-b stays unclicked.
	if q.Q != "retention" || q.Searches != 2 || q.WithResults != 2 || q.NoClick != 1 || q.ShownTimes != 2 ||
		len(q.Shown) != 3 || len(q.Clicks) != 1 || q.Clicks[0].ID != "c" || q.Clicks[0].Ranks["3"] != 1 || !q.LowRank() ||
		len(q.Revs) != 2 || len(q.Versions) != 2 || q.FirstDay != "2026-11-20" || q.LastDay != "2026-11-25" {
		t.Fatalf("retention = %+v", q)
	}
	if z := r.Queries[1]; z.Q != "teams chat" || z.ZeroResults != 1 || z.NoClick != 0 || z.LowRank() {
		t.Fatalf("zero = %+v", z)
	}
}

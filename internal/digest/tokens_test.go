package digest

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/replypen/replypen-cli/internal/client"
)

func tokItems(t *testing.T, js string) []client.Item {
	t.Helper()
	var items []client.Item
	if err := json.Unmarshal([]byte(js), &items); err != nil {
		t.Fatal(err)
	}
	return items
}

func ids(items []client.Item) string {
	var s []string
	for _, it := range items {
		s = append(s, TokenString(it, "id"))
	}
	return strings.Join(s, ",")
}

func TestSelectTokens(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	items := tokItems(t, `[
	 {"id":"recent","status":"active","last_used_at":"2026-10-12T09:00:00Z"},
	 {"id":"edge30","status":"active","last_used_at":"2026-09-12T12:00:00Z"},
	 {"id":"stale","status":"active","last_used_at":"2026-08-01T00:00:00Z"},
	 {"id":"never_new","status":"active","created_at":"2026-10-10T00:00:00Z","last_used_at":null},
	 {"id":"never_old","status":"active","created_at":"2026-05-01T00:00:00Z"},
	 {"id":"old_shape","status":"active"},
	 {"id":"revoked","status":"revoked"},
	 {"id":"garbled","status":"active","last_used_at":"yesterday"}
	]`)
	for _, tc := range []struct {
		name string
		f    TokenFilter
		want string
	}{
		{"default active, LRU first", TokenFilter{Now: now}, "garbled,old_shape,never_old,never_new,stale,edge30,recent"},
		{"include revoked", TokenFilter{Now: now, IncludeInactive: true}, "garbled,old_shape,revoked,never_old,never_new,stale,edge30,recent"},
		{"unused 30d keeps exactly-30d row out", TokenFilter{Now: now, UnusedDays: 30}, "garbled,old_shape,never_old,never_new,stale"},
	} {
		if got := ids(SelectTokens(items, tc.f)); got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.name, got, tc.want)
		}
	}
	if len(items) != 8 || TokenString(items[0], "id") != "recent" {
		t.Error("input was modified")
	}
}

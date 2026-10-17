package digest

import "testing"

func TestChatCardPending(t *testing.T) {
	cases := map[string]string{
		`{"status":"proposed","preview_state":"building","review_state":"pending"}`:  "preview building",
		`{"status":"proposed","preview_state":"ready","review_state":"reviewing"}`:   "review reviewing",
		`{"status":"proposed","preview_state":"ready","review_state":"reviewed"}`:    "",
		`{"status":"proposed","preview_state":"failed","review_state":"unverified"}`: "",
		`{"status":"published","live_state":"building","review_state":"reviewing"}`:  "live site building",
		`{"status":"published","live_state":"ready"}`:                                "",
		`{"status":"declined","preview_state":"building"}`:                           "",
		`{"status":"proposed"}`: "",
		`not json`:              "",
	}
	for raw, want := range cases {
		if got := ChatCardPending([]byte(raw)); got != want {
			t.Errorf("ChatCardPending(%s) = %q, want %q", raw, got, want)
		}
	}
}

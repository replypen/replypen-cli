package digest

import "encoding/json"

// ChatCardPending names what a chat card projection is still waiting on ("" = nothing in flight), so
// `chat card --wait` can stop at the state a human would act on. It reads the website-change card's
// lifecycle fields (server internal/chatwebsite): the branch preview building, the host's own review
// turn, the live build after Publish. A card without those fields is never in flight.
func ChatCardPending(raw json.RawMessage) string {
	var card struct {
		Status       string `json:"status"`
		PreviewState string `json:"preview_state"`
		ReviewState  string `json:"review_state"`
		LiveState    string `json:"live_state"`
	}
	if json.Unmarshal(raw, &card) != nil {
		return ""
	}
	switch card.Status {
	case "proposed":
		if card.PreviewState == "building" {
			return "preview building"
		}
		switch card.ReviewState {
		case "pending", "shooting", "reviewing":
			return "review " + card.ReviewState
		}
	case "published":
		if card.LiveState == "building" {
			return "live site building"
		}
	}
	return ""
}

package digest

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/replypen/replypen-cli/internal/client"
)

// Depth is the args of the host's one synthetic `tool:"host" status:"depth"` event per chat run: which
// tier (standard = Quick, pro = Deeper) answered and why. Choice/PSimple/Confidence are set only when
// the router ran. The event's seq (3.04M) sorts it near the END of the list; renderers lift it to the top.
type Depth struct {
	Tier         string   `json:"tier"`
	Source       string   `json:"source"`
	Choice       string   `json:"choice,omitempty"`
	PSimple      *float64 `json:"p_simple,omitempty"`
	Confidence   *float64 `json:"confidence,omitempty"`
	PreviousTier string   `json:"previous_tier,omitempty"`
	Switched     bool     `json:"switched,omitempty"`
}

// IsDepthEvent reports whether a run event is the host's depth decision row.
func IsDepthEvent(tool, status string) bool { return tool == "host" && status == "depth" }

// ParseDepth decodes a depth event's args; false when absent or unreadable.
func ParseDepth(args json.RawMessage) (Depth, bool) {
	var d Depth
	if len(args) == 0 || json.Unmarshal(args, &d) != nil || d.Tier == "" {
		return Depth{}, false
	}
	return d, true
}

// DepthOf finds the run's depth decision among its trace events.
func DepthOf(events []client.EventItem) (Depth, bool) {
	for _, e := range events {
		if IsDepthEvent(e.Tool, e.Status) {
			return ParseDepth(e.Args)
		}
	}
	return Depth{}, false
}

// Line is the shared operator one-liner, e.g.
// `depth: pro (switched from standard; router: substantial p=0.01)`.
func (d Depth) Line() string {
	src := d.Source
	if src == "" {
		src = "unknown"
	}
	if src == "router" && d.Choice != "" {
		src += ": " + d.Choice
		if d.PSimple != nil {
			src += fmt.Sprintf(" p=%.2f", *d.PSimple)
		}
	}
	if d.Switched && d.PreviousTier != "" {
		src = "switched from " + d.PreviousTier + "; " + src
	}
	return "depth: " + d.Tier + " (" + src + ")"
}

// LiftDepth returns events with the depth row moved to the front (stable otherwise), so the decision
// reads before the turns it shaped.
func LiftDepth[T any](events []T, isDepth func(T) bool) []T {
	idx := -1
	for i, e := range events {
		if isDepth(e) {
			idx = i
			break
		}
	}
	if idx <= 0 {
		return events
	}
	out := make([]T, 0, len(events))
	out = append(out, events[idx])
	out = append(out, events[:idx]...)
	return append(out, events[idx+1:]...)
}

// TierLabel is the terse sessions-list cell: the tier, `*` marking a member's explicit pick.
func TierLabel(tier, source string) string {
	tier = strings.TrimSpace(tier)
	if tier == "" {
		return "-"
	}
	if source == "user" {
		return tier + "*"
	}
	return tier
}

package digest

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/rootcause-org/rootcause-cli/internal/client"
)

// TokenFilter selects and orders `rc project token ls` rows (AWS-IAM-style last-used review).
type TokenFilter struct {
	// IncludeInactive keeps non-active (revoked) tokens; the default keeps active ones only.
	IncludeInactive bool
	// UnusedDays > 0 keeps only tokens never used or last used more than that many days ago.
	UnusedDays int
	Now        time.Time
}

// SelectTokens filters then sorts least-recently-used first: never-used tokens, then oldest last use;
// ties by created_at, then id. Rows from a server that predates last-used tracking lack the keys and
// simply read as never used. The input slice is not modified.
func SelectTokens(items []client.Item, f TokenFilter) []client.Item {
	out := make([]client.Item, 0, len(items))
	for _, it := range items {
		if !f.IncludeInactive && !TokenActive(it) {
			continue
		}
		if f.UnusedDays > 0 {
			if last, ok := TokenTime(it, "last_used_at"); ok && !last.Before(f.Now.AddDate(0, 0, -f.UnusedDays)) {
				continue
			}
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool {
		li, iok := TokenTime(out[i], "last_used_at")
		lj, jok := TokenTime(out[j], "last_used_at")
		if iok != jok {
			return !iok // never used first
		}
		if iok && !li.Equal(lj) {
			return li.Before(lj)
		}
		ci, _ := TokenTime(out[i], "created_at")
		cj, _ := TokenTime(out[j], "created_at")
		if !ci.Equal(cj) {
			return ci.Before(cj)
		}
		return TokenString(out[i], "id") < TokenString(out[j], "id")
	})
	return out
}

// TokenActive: a row without a status counts as active (only an explicit non-active status hides it).
func TokenActive(it client.Item) bool {
	s := TokenString(it, "status")
	return s == "" || s == "active"
}

// TokenString reads a string field; absent, null and non-string values read as "".
func TokenString(it client.Item, key string) string {
	var s string
	if raw, ok := it[key]; ok && json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

// TokenTime parses an RFC3339 field; absent, null or unparseable reports ok=false.
func TokenTime(it client.Item, key string) (time.Time, bool) {
	s := TokenString(it, key)
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, s)
	return t, err == nil
}

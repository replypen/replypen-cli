package digest

import (
	"sort"
	"strconv"
	"strings"

	"github.com/replypen/replypen-cli/internal/client"
)

// SearchQuery is one distinct (case-folded, server-redacted) help-center query over the window: what the
// weekly help-search tune-up replays and judges.
type SearchQuery struct {
	Q           string `json:"q"`
	Searches    int    `json:"searches"`
	ZeroResults int    `json:"zero_results"`
	WithResults int    `json:"with_results"`
	// NoClick: searches WITH results that no click joined (by sid).
	NoClick int `json:"no_click"`
	// Shown: the most frequent result list visitors saw, in display order; ShownTimes how often.
	Shown      []string      `json:"shown"`
	ShownTimes int           `json:"shown_times"`
	Clicks     []SearchClick `json:"clicks"`
	Revs       []string      `json:"revs"`
	Versions   []int         `json:"versions"`
	FirstDay   string        `json:"first_day"`
	LastDay    string        `json:"last_day"`
}

type SearchClick struct {
	ID     string         `json:"id"`
	Clicks int            `json:"clicks"`
	Ranks  map[string]int `json:"ranks"` // 1-based rank → clicks
}

type SearchReport struct {
	Project   string        `json:"project"`
	From      string        `json:"from"`
	To        string        `json:"to"`
	Truncated bool          `json:"truncated"`
	Searches  int           `json:"searches"`
	Clicks    int           `json:"clicks"`
	Queries   []SearchQuery `json:"queries"`
}

type searchAcc struct {
	*SearchQuery
	lists      map[string]int
	clicks     map[string]*SearchClick
	clickedSid map[string]bool
	withSids   []string
	revs       map[string]bool
	versions   map[int]bool
}

// SearchReportFrom aggregates raw events (oldest first) per query, ordered by volume. A click joins its search
// by sid; when that search fell outside the window, the click's own q is the key.
func SearchReportFrom(l *client.SearchEventList) SearchReport {
	out := SearchReport{Project: l.Project, From: l.From, To: l.To, Truncated: l.Truncated, Queries: []SearchQuery{}}
	accs := map[string]*searchAcc{}
	bySid := map[string]*searchAcc{}
	get := func(q string) *searchAcc {
		key := strings.ToLower(q)
		a := accs[key]
		if a == nil {
			a = &searchAcc{SearchQuery: &SearchQuery{Q: key}, lists: map[string]int{}, clicks: map[string]*SearchClick{},
				clickedSid: map[string]bool{}, revs: map[string]bool{}, versions: map[int]bool{}}
			accs[key] = a
		}
		return a
	}
	for _, e := range l.Items {
		var a *searchAcc
		switch e.Kind {
		case "search":
			a = get(e.Q)
			bySid[e.Sid] = a
			out.Searches++
			a.Searches++
			if e.N != nil && *e.N == 0 {
				a.ZeroResults++
			} else {
				a.WithResults++
				a.withSids = append(a.withSids, e.Sid)
			}
			a.lists[strings.Join(e.Shown, "\x00")]++
		case "click":
			if a = bySid[e.Sid]; a == nil {
				a = get(e.Q)
			}
			out.Clicks++
			a.clickedSid[e.Sid] = true
			id := ""
			if e.Clicked != nil {
				id = *e.Clicked
			}
			c := a.clicks[id]
			if c == nil {
				c = &SearchClick{ID: id, Ranks: map[string]int{}}
				a.clicks[id] = c
			}
			c.Clicks++
			if e.Rank != nil {
				c.Ranks[strconv.Itoa(*e.Rank)]++
			}
		default:
			continue
		}
		if e.Rev != "" {
			a.revs[e.Rev] = true
		}
		if e.V != nil {
			a.versions[*e.V] = true
		}
		day := e.At
		if len(day) > 10 {
			day = day[:10]
		}
		if a.FirstDay == "" {
			a.FirstDay = day
		}
		a.LastDay = day
	}
	for _, a := range accs {
		for _, sid := range a.withSids {
			if !a.clickedSid[sid] {
				a.NoClick++
			}
		}
		best, n := "", 0
		for k, c := range a.lists {
			if c > n || (c == n && k < best) {
				best, n = k, c
			}
		}
		a.Shown, a.ShownTimes = []string{}, n
		if best != "" {
			a.Shown = strings.Split(best, "\x00")
		}
		a.Clicks = []SearchClick{}
		for _, c := range a.clicks {
			a.Clicks = append(a.Clicks, *c)
		}
		sort.Slice(a.Clicks, func(i, j int) bool {
			if a.Clicks[i].Clicks != a.Clicks[j].Clicks {
				return a.Clicks[i].Clicks > a.Clicks[j].Clicks
			}
			return a.Clicks[i].ID < a.Clicks[j].ID
		})
		a.Revs = []string{}
		for r := range a.revs {
			a.Revs = append(a.Revs, r)
		}
		sort.Strings(a.Revs)
		a.Versions = []int{}
		for v := range a.versions {
			a.Versions = append(a.Versions, v)
		}
		sort.Ints(a.Versions)
		out.Queries = append(out.Queries, *a.SearchQuery)
	}
	sort.Slice(out.Queries, func(i, j int) bool {
		if out.Queries[i].Searches != out.Queries[j].Searches {
			return out.Queries[i].Searches > out.Queries[j].Searches
		}
		return out.Queries[i].Q < out.Queries[j].Q
	})
	return out
}

// LowRank reports whether a query's clicks include one at rank 3 or lower.
func (q SearchQuery) LowRank() bool {
	for _, c := range q.Clicks {
		for r := range c.Ranks {
			if n, err := strconv.Atoi(r); err == nil && n >= 3 {
				return true
			}
		}
	}
	return false
}

package webapp

import (
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"jaygoel.com/plainoldanalytics/storage"
)

// rangeParam returns the current request's query string with "range"
// removed, suitable for prefixing a new "range=" value so the range
// switcher preserves every other filter. Non-empty results end in "&".
func rangeParam(r *http.Request) string {
	return queryWithout(r, "range")
}

// queryWithout returns the current request's query string with key removed,
// suitable for prefixing a new "key=" value so a single-choice link (a tab,
// a range) preserves every other filter without ever duplicating key
// itself. Non-empty results end in "&".
func queryWithout(r *http.Request, key string) string {
	q := r.URL.Query()
	q.Del(key)
	if len(q) == 0 {
		return ""
	}
	return q.Encode() + "&"
}

// timelineItem is one entry of a replay or visitor-activity timeline: a
// request or a custom event, merged and ordered by time.
type timelineItem struct {
	At    time.Time
	Kind  string // "req", "err" (a request with a 4xx/5xx status), or "ev"
	Label string
	Sub   string
}

// buildTimeline merges a session's (or visitor's) requests and events into
// one chronological timeline for the replay sidebar and visitor activity
// feed.
func buildTimeline(traffic []storage.Traffic, events []storage.Event) []timelineItem {
	items := make([]timelineItem, 0, len(traffic)+len(events))
	for _, t := range traffic {
		kind := "req"
		if t.Status >= 400 {
			kind = "err"
		}
		items = append(items, timelineItem{
			At: t.Time, Kind: kind,
			Label: t.Method + " " + t.Pattern,
			Sub:   fmt.Sprintf("%d · %s", t.Status, t.Duration),
		})
	}
	for _, e := range events {
		items = append(items, timelineItem{
			At: e.Time, Kind: "ev", Label: e.Name, Sub: propsText(e.Props),
		})
	}
	slices.SortFunc(items, func(a, b timelineItem) int { return a.At.Compare(b.At) })
	return items
}

// propsText renders an event's props compactly for list display, e.g.
// "plan pro · source docs".
func propsText(props map[string]any) string {
	if len(props) == 0 {
		return ""
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s %v", k, props[k])
	}
	return strings.Join(parts, " · ")
}

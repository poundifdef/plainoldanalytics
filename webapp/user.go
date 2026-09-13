package webapp

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"jaygoel.com/plainoldanalytics/storage"
)

// userPage aggregates every visitor (browser/device) sharing one user
// identity — the destination for every "this is a known user" link in the
// UI. It differs from visitorPage, which is scoped to a single browser: one
// user may sign in from several, and this is where they come back together.
func (a *App) userPage(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()

	traffic, err := a.store.Traffic(ctx, storage.TrafficFilter{ContextKey: a.userKey, ContextValue: name})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sessions, err := a.store.Sessions(ctx, storage.SessionFilter{
		ContextKey: a.userKey, ContextValue: name, AnnotateKey: a.userKey,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(traffic) == 0 && len(sessions) == 0 {
		http.NotFound(w, r)
		return
	}

	// Events carry no context, so there's no way to query them by user
	// directly — collect the user's distinct visitor IDs from traffic and
	// sessions instead, then fetch each visitor's events.
	visitorSet := map[uint64]bool{}
	for _, t := range traffic {
		if t.Visitor != 0 {
			visitorSet[t.Visitor] = true
		}
	}
	for _, s := range sessions {
		if s.Visitor != 0 {
			visitorSet[s.Visitor] = true
		}
	}
	visitors := make([]uint64, 0, len(visitorSet))
	for vid := range visitorSet {
		visitors = append(visitors, vid)
	}
	slices.Sort(visitors)

	var events []storage.Event
	for _, vid := range visitors {
		ev, err := a.store.Events(ctx, storage.EventFilter{Visitor: vid})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		events = append(events, ev...)
	}

	var first, last time.Time
	errCount := 0
	for i, t := range traffic {
		if i == 0 || t.Time.Before(first) {
			first = t.Time
		}
		if i == 0 || t.Time.After(last) {
			last = t.Time
		}
		if t.Status >= 400 {
			errCount++
		}
	}

	stats := []visitorStat{
		{"Visitors", strconv.Itoa(len(visitors))},
		{"Sessions", strconv.Itoa(len(sessions))},
		{"Requests", strconv.Itoa(len(traffic))},
		{"Events", strconv.Itoa(len(events))},
		{"Errors", strconv.Itoa(errCount)},
	}
	if !first.IsZero() {
		stats = append([]visitorStat{
			{"First seen", first.Format("2006-01-02 15:04:05")},
			{"Last seen", last.Format("2006-01-02 15:04:05")},
		}, stats...)
	}

	activity := buildTimeline(traffic, events)
	slices.Reverse(activity)
	if len(activity) > 60 {
		activity = activity[:60]
	}

	a.render(w, r, "user", map[string]any{
		"Nav": "traffic", "Crumb": name,
		"User": name, "Visitors": visitors,
		"Stats": stats, "Sessions": sessions, "Activity": activity,
	})
}

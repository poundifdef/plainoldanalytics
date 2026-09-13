package webapp

import (
	"net/http"
	"slices"
	"strconv"
	"time"

	"jaygoel.com/plainoldanalytics/storage"
)

// visitorStat is one cell of the visitor page's stats row.
type visitorStat struct {
	Label string
	Value string
}

// visitorPage aggregates one visitor's traffic, sessions, and events: the
// destination for "open visitor" links from traffic, sessions, and events.
func (a *App) visitorPage(w http.ResponseWriter, r *http.Request) {
	vid := parseID(r.PathValue("id"))
	if vid == 0 {
		http.NotFound(w, r)
		return
	}
	ctx := r.Context()

	traffic, err := a.store.Traffic(ctx, storage.TrafficFilter{Visitor: vid})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sessions, err := a.store.Sessions(ctx, storage.SessionFilter{Visitor: vid, AnnotateKey: a.userKey})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	events, err := a.store.Events(ctx, storage.EventFilter{Visitor: vid})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if len(traffic) == 0 && len(sessions) == 0 && len(events) == 0 {
		http.NotFound(w, r)
		return
	}

	var user string
	for _, s := range sessions {
		if s.Annotation != "" {
			user = s.Annotation
			break
		}
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

	a.render(w, r, "visitor", map[string]any{
		"Nav": "traffic", "Crumb": visitorCrumb(user, vid),
		"VisitorID": vid, "User": user,
		"Stats": stats, "Sessions": sessions, "Activity": activity,
	})
}

func visitorCrumb(user string, id uint64) string {
	if user != "" {
		return user
	}
	return "visitor " + strconv.FormatUint(id, 10)
}

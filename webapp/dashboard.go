package webapp

import (
	"cmp"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"slices"
	"time"

	"jaygoel.com/plainoldanalytics/storage"
)

// dashRange is one selectable dashboard time window.
type dashRange struct {
	Key    string        // query-param value, e.g. "24h"
	Label  string        // switcher text, e.g. "24 hours"
	Window time.Duration // how far back the window reaches
	Bucket time.Duration // chart bucket size
	Format string        // bar label time format
}

var dashRanges = []dashRange{
	{"24h", "24 hours", 24 * time.Hour, time.Hour, "15:04"},
	{"7d", "7 days", 7 * 24 * time.Hour, 6 * time.Hour, "Mon 2"},
	{"30d", "30 days", 30 * 24 * time.Hour, 24 * time.Hour, "Jan 2"},
}

func pickRange(key string) dashRange {
	for _, dr := range dashRanges {
		if dr.Key == key {
			return dr
		}
	}
	return dashRanges[0]
}

// statRow is one row of a breakdown card: a value, its request count, and
// the bar width relative to the card's largest row.
type statRow struct {
	Name  string
	Count int
	W     float64 // percent of the card's max count
	Link  string  // optional drill-down URL (relative to the mount root)
}

// dashCard is one breakdown card ("Pages", "Referrers", ...).
type dashCard struct {
	Title string
	Rows  []statRow
}

func (a *App) dashboardPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	dr := pickRange(r.FormValue("range"))
	since := time.Now().Add(-dr.Window)

	stats, err := a.store.TrafficStats(ctx, since, a.userKey)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	series, err := a.store.TrafficSeries(ctx, since, int(dr.Bucket.Minutes()))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sessions, err := a.store.Sessions(ctx, storage.SessionFilter{})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	events, err := a.store.Events(ctx, storage.EventFilter{From: since})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// The store doesn't window sessions; keep those overlapping the range.
	nSessions, avg := 0, time.Duration(0)
	for _, s := range sessions {
		if s.End.Before(since) {
			continue
		}
		nSessions++
		avg += s.End.Sub(s.Start)
	}
	if nSessions > 0 {
		avg /= time.Duration(nSessions)
	}

	browsers, oses, devices := map[string]int{}, map[string]int{}, map[string]int{}
	for _, ua := range stats.UserAgents {
		info := parseUA(ua.Name)
		browsers[info.Browser] += ua.Count
		oses[info.OS] += ua.Count
		devices[info.Device] += ua.Count
	}

	// Referrers collapse to their host; same-site navigation (host equal to
	// the dashboard's own) is noise, not acquisition, and is dropped.
	referrers := map[string]int{}
	self := hostOnly(r.Host)
	for _, ref := range stats.Referrers {
		if host := refHost(ref.Name); host != self {
			referrers[host] += ref.Count
		}
	}

	pages := topRows(stats.Pages, 10)
	for i, p := range pages {
		pages[i].Link = "traffic?pattern=" + url.QueryEscape(p.Name)
	}
	users := topRows(stats.Users, 10)
	for i, u := range users {
		users[i].Link = "users/" + url.PathEscape(u.Name)
	}

	cards := []dashCard{
		{"Pages", pages},
		{"Referrers", topRows(mapCounts(referrers), 10)},
		{"Browsers", topRows(mapCounts(browsers), 10)},
		{"Operating systems", topRows(mapCounts(oses), 10)},
		{"Devices", topRows(mapCounts(devices), 10)},
	}
	if len(users) > 0 {
		cards = append(cards, dashCard{"Users", users})
	}

	a.render(w, r, "dashboard", map[string]any{
		"Nav":        "overview",
		"Range":      dr,
		"Ranges":     dashRanges,
		"Empty":      stats.Views == 0,
		"Views":      stats.Views,
		"Visitors":   stats.Visitors,
		"Sessions":   nSessions,
		"AvgSession": formatDuration(avg),
		"Events":     len(events),
		"Bars":       buildBars(series, since, dr),
		"Cards":      cards,
	})
}

// chartBar is one bar of the dashboard's server-rendered SVG chart.
type chartBar struct {
	X, Y, W, H float64
	Label      string
	Count      int
}

// buildBars lays out a fixed 720x150 bar chart over the range's window,
// filling empty buckets so gaps show as gaps. The extra bucket holds the
// current, still-filling one: the first bucket starts at or before since,
// so "now" falls one bucket past the window's worth.
func buildBars(points []storage.SeriesPoint, since time.Time, dr dashRange) []chartBar {
	n := int(dr.Window/dr.Bucket) + 1
	counts := make([]int, n)
	labels := make([]string, n)
	start := since.Truncate(dr.Bucket)
	for i := range n {
		labels[i] = start.Add(time.Duration(i) * dr.Bucket).Local().Format(dr.Format)
	}
	for _, p := range points {
		if i := int(p.Bucket.Sub(start) / dr.Bucket); i >= 0 && i < n {
			counts[i] += p.Count
		}
	}
	max := 1
	for _, c := range counts {
		if c > max {
			max = c
		}
	}
	const width, height = 720.0, 150.0
	bw := width / float64(n)
	bars := make([]chartBar, n)
	for i, c := range counts {
		h := height * float64(c) / float64(max)
		bars[i] = chartBar{
			X: float64(i) * bw, Y: height - h, W: bw - 2, H: h,
			Label: labels[i], Count: c,
		}
	}
	return bars
}

// topRows converts a breakdown to display rows, sizing each row's bar
// relative to the largest count in the list.
func topRows(counts []storage.NameCount, limit int) []statRow {
	if len(counts) > limit {
		counts = counts[:limit]
	}
	max := 1
	for _, c := range counts {
		if c.Count > max {
			max = c.Count
		}
	}
	rows := make([]statRow, len(counts))
	for i, c := range counts {
		rows[i] = statRow{Name: c.Name, Count: c.Count, W: 100 * float64(c.Count) / float64(max)}
	}
	return rows
}

// mapCounts converts an aggregation map to a NameCount list ordered by
// count descending, ties by name.
func mapCounts(m map[string]int) []storage.NameCount {
	counts := make([]storage.NameCount, 0, len(m))
	for name, count := range m {
		counts = append(counts, storage.NameCount{Name: name, Count: count})
	}
	slices.SortFunc(counts, func(a, b storage.NameCount) int {
		return cmp.Or(cmp.Compare(b.Count, a.Count), cmp.Compare(a.Name, b.Name))
	})
	return counts
}

// refHost extracts the host of a Referer value ("https://x.com/p" ->
// "x.com"); relative or unparseable referrers return the raw string.
func refHost(ref string) string {
	u, err := url.Parse(ref)
	if err != nil || u.Host == "" {
		return ref
	}
	return u.Hostname()
}

// hostOnly strips the port from a host:port form, if present.
func hostOnly(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	return hostport
}

// formatDuration renders a session duration compactly: "45s", "4m 30s",
// "1h 12m".
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

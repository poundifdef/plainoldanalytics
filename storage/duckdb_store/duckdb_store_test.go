package duckdb_store

import (
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	goduckdb "github.com/duckdb/duckdb-go/v2"
	"jaygoel.com/plainoldanalytics/storage"
)

func newTestDB(t *testing.T) *DuckDB {
	t.Helper()
	connector, err := goduckdb.NewConnector("", nil)
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(connector)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestTraffic(t *testing.T) {
	d := newTestDB(t)

	req := httptest.NewRequest("GET", "/b/x?foo=1&foo=2&bar=z", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	req.Header.Set("User-Agent", "test-agent")
	req.Header.Add("Accept", "text/html")
	req.Header.Add("Accept", "application/json")
	ts := time.Date(2026, 8, 8, 12, 30, 45, 123456000, time.UTC)
	d.Record(req, storage.Capture{
		Pattern:    "/b/{c}",
		Status:     200,
		Size:       1,
		Time:       ts,
		Duration:   1500 * time.Microsecond,
		PathValues: map[string]string{"c": "x"},
		Visitor:    111,
		Context:    map[string]string{"user": "jay"},
	})

	traffic, err := d.Traffic(t.Context(), storage.TrafficFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(traffic) != 1 || traffic[0].ID == 0 {
		t.Fatalf("Traffic: want 1 row with an ID, got %+v", traffic)
	}
	want := storage.Traffic{
		ID:          traffic[0].ID, // random per record
		Method:      "GET",
		Pattern:     "/b/{c}",
		QueryParams: map[string][]string{"foo": {"1", "2"}, "bar": {"z"}},
		PathParams:  map[string]string{"c": "x"},
		Headers: map[string][]string{
			"User-Agent": {"test-agent"},
			"Accept":     {"text/html", "application/json"},
		},
		Status:   200,
		Size:     1,
		Time:     ts,
		Duration: 1500 * time.Microsecond,
		IP:       "1.2.3.4",
		Visitor:  111,
		Context:  map[string]string{"user": "jay"},
	}
	if !reflect.DeepEqual(traffic[0], want) {
		t.Errorf("Traffic:\ngot  %+v\nwant %+v", traffic[0], want)
	}

	got, err := d.TrafficByID(t.Context(), traffic[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, traffic[0]) {
		t.Errorf("TrafficByID:\ngot  %+v\nwant %+v", got, traffic[0])
	}
	if _, err := d.TrafficByID(t.Context(), 424242); err != storage.ErrNotFound {
		t.Errorf("unknown id: got %v, want ErrNotFound", err)
	}

	// Filters.
	for name, f := range map[string]storage.TrafficFilter{
		"by context": {ContextKey: "user", ContextValue: "jay"},
		"by visitor": {Visitor: 111},
		"by pattern": {Pattern: "/b/"},
		"by status":  {Status: 200},
	} {
		rows, err := d.Traffic(t.Context(), f)
		if err != nil || len(rows) != 1 {
			t.Errorf("filter %s: rows=%d err=%v", name, len(rows), err)
		}
	}
	rows, err := d.Traffic(t.Context(), storage.TrafficFilter{ContextKey: "user", ContextValue: "nobody"})
	if err != nil || len(rows) != 0 {
		t.Errorf("filter miss: rows=%d err=%v", len(rows), err)
	}

	series, err := d.TrafficSeries(t.Context(), ts.Add(-time.Hour), 60)
	if err != nil {
		t.Fatal(err)
	}
	sum := 0
	for _, p := range series {
		sum += p.Count
	}
	if sum != 1 {
		t.Errorf("series: total %d, want 1", sum)
	}
}

func TestTrafficStats(t *testing.T) {
	d := newTestDB(t)
	now := time.Now()

	rec := func(pattern, referer, ua string, visitor uint64, user string) {
		req := httptest.NewRequest("GET", "/x", nil)
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		if ua != "" {
			req.Header.Set("User-Agent", ua)
		}
		var ctxVals map[string]string
		if user != "" {
			ctxVals = map[string]string{"user": user}
		}
		d.Record(req, storage.Capture{
			Pattern: pattern, Status: 200, Time: now, Visitor: visitor, Context: ctxVals,
		})
	}
	rec("/", "https://google.com/", "Chrome", 1, "jay")
	rec("/", "", "curl/8.4.0", 1, "")
	rec("/pricing", "https://google.com/", "", 2, "sam")
	// Outside the window: must not count anywhere.
	d.Record(httptest.NewRequest("GET", "/x", nil), storage.Capture{
		Pattern: "/old", Status: 200, Time: now.Add(-48 * time.Hour), Visitor: 3,
	})

	s, err := d.TrafficStats(t.Context(), now.Add(-time.Hour), "user")
	if err != nil {
		t.Fatal(err)
	}
	if s.Views != 3 || s.Visitors != 2 {
		t.Errorf("views/visitors: got %d/%d, want 3/2", s.Views, s.Visitors)
	}
	wantPages := []storage.NameCount{{Name: "/", Count: 2}, {Name: "/pricing", Count: 1}}
	if !reflect.DeepEqual(s.Pages, wantPages) {
		t.Errorf("pages: got %+v, want %+v", s.Pages, wantPages)
	}
	wantRefs := []storage.NameCount{{Name: "https://google.com/", Count: 2}}
	if !reflect.DeepEqual(s.Referrers, wantRefs) {
		t.Errorf("referrers: got %+v, want %+v", s.Referrers, wantRefs)
	}
	wantUAs := []storage.NameCount{{Name: "Chrome", Count: 1}, {Name: "curl/8.4.0", Count: 1}}
	if !reflect.DeepEqual(s.UserAgents, wantUAs) {
		t.Errorf("user agents: got %+v, want %+v", s.UserAgents, wantUAs)
	}
	wantUsers := []storage.NameCount{{Name: "jay", Count: 1}, {Name: "sam", Count: 1}}
	if !reflect.DeepEqual(s.Users, wantUsers) {
		t.Errorf("users: got %+v, want %+v", s.Users, wantUsers)
	}

	// No user key: the Users breakdown is skipped.
	s, err = d.TrafficStats(t.Context(), now.Add(-time.Hour), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Users) != 0 {
		t.Errorf("users without key: got %+v, want none", s.Users)
	}
}

func TestIdentifyBackfillsOnlyTheFirstKnownUser(t *testing.T) {
	d := newTestDB(t)
	req := httptest.NewRequest("GET", "/", nil)
	now := time.Now()

	// Anonymous activity belongs to this long-lived browser identity.
	d.Record(req, storage.Capture{Pattern: "/", Status: 200, Time: now, Visitor: 111, Session: 1})
	if err := d.Identify(t.Context(), 111, "user", "jay@example.com"); err != nil {
		t.Fatal(err)
	}
	rows, err := d.Traffic(t.Context(), storage.TrafficFilter{ContextKey: "user", ContextValue: "jay@example.com"})
	if err != nil || len(rows) != 1 {
		t.Fatalf("first identity did not backfill anonymous traffic: rows=%+v err=%v", rows, err)
	}

	// The browser can later carry a different signed-in user. Identifying it
	// must not rewrite the history already assigned to Jay.
	if err := d.Identify(t.Context(), 111, "user", "sam@example.com"); err != nil {
		t.Fatal(err)
	}
	rows, err = d.Traffic(t.Context(), storage.TrafficFilter{ContextKey: "user", ContextValue: "sam@example.com"})
	if err != nil || len(rows) != 0 {
		t.Fatalf("later identity rewrote history: rows=%+v err=%v", rows, err)
	}
}

func TestEventsAndSessions(t *testing.T) {
	d := newTestDB(t)
	ctx := t.Context()

	t0 := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

	// Visitor 111: two activity bursts more than 30 minutes apart, so they
	// derive into two sessions; the first has an event and a replay.
	req := httptest.NewRequest("GET", "/", nil)
	d.Record(req, storage.Capture{
		Pattern: "/", Status: 200, Time: t0,
		Visitor: 111, Session: 1001, Context: map[string]string{"user": "jay"},
	})
	if err := d.TrackEvent(ctx, storage.Event{
		Name: "button_click", Props: map[string]any{"where": "demo"},
		Visitor: 111, Path: "/", Time: t0.Add(5 * time.Minute),
		Session: 1001,
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveReplay(ctx, storage.ReplayChunk{
		Visitor: 111, Time: t0.Add(6 * time.Minute), Data: []byte(`[{"type":4},{"type":2}]`),
		Session: 1001,
	}); err != nil {
		t.Fatal(err)
	}
	d.Record(req, storage.Capture{
		Pattern: "/", Status: 200, Time: t0.Add(50 * time.Minute), Visitor: 111, Session: 1002,
	})

	// Visitor 222: one lone event.
	if err := d.TrackEvent(ctx, storage.Event{
		Name: "pageview", Visitor: 222, Session: 2001, Time: t0,
	}); err != nil {
		t.Fatal(err)
	}

	events, err := d.Events(ctx, storage.EventFilter{Name: "button_click"})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Visitor != 111 || events[0].Props["where"] != "demo" {
		t.Fatalf("events: %+v", events)
	}

	// All sessions: two for visitor 111 (split by the 44-minute gap), one
	// for visitor 222.
	sessions, err := d.Sessions(ctx, storage.SessionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 3 {
		t.Fatalf("sessions: want 3, got %+v", sessions)
	}

	// First 111 session: traffic + event + replay in one window.
	sessions, err = d.Sessions(ctx, storage.SessionFilter{Visitor: 111})
	if err != nil || len(sessions) != 2 {
		t.Fatalf("sessions of visitor: %+v err=%v", sessions, err)
	}
	first := sessions[1] // ordered newest-first
	if !first.Start.Equal(t0) || !first.End.Equal(t0.Add(6*time.Minute)) {
		t.Errorf("first session window: %+v", first)
	}
	if first.TrafficCount != 1 || first.EventCount != 1 || !first.HasReplay {
		t.Errorf("first session summary: %+v", first)
	}
	if sessions[0].HasReplay || sessions[0].TrafficCount != 1 {
		t.Errorf("second session summary: %+v", sessions[0])
	}

	// "for user jay, show me their sessions" — user is just a context pair;
	// only the burst containing the context-tagged request matches.
	sessions, err = d.Sessions(ctx, storage.SessionFilter{
		ContextKey: "user", ContextValue: "jay", AnnotateKey: "user",
	})
	if err != nil || len(sessions) != 1 || sessions[0].Visitor != 111 {
		t.Fatalf("sessions by context: %+v err=%v", sessions, err)
	}
	if sessions[0].Annotation != "jay" {
		t.Errorf("annotation: got %q, want jay", sessions[0].Annotation)
	}

	// "for all sessions where event X occurred, pull up their sessions"
	sessions, err = d.Sessions(ctx, storage.SessionFilter{EventName: "button_click"})
	if err != nil || len(sessions) != 1 || sessions[0].Visitor != 111 {
		t.Fatalf("sessions by event: %+v err=%v", sessions, err)
	}
	sessions, err = d.Sessions(ctx, storage.SessionFilter{EventName: "no_such_event"})
	if err != nil || len(sessions) != 0 {
		t.Fatalf("sessions by missing event: %+v err=%v", sessions, err)
	}

	// Replay chunks fetched by the derived session's window.
	chunks, err := d.Replay(ctx, 111, t0, t0.Add(6*time.Minute))
	if err != nil || len(chunks) != 1 || string(chunks[0].Data) != `[{"type":4},{"type":2}]` {
		t.Fatalf("replay chunks: %+v err=%v", chunks, err)
	}
	chunks, err = d.Replay(ctx, 111, t0.Add(40*time.Minute), t0.Add(time.Hour))
	if err != nil || len(chunks) != 0 {
		t.Fatalf("replay outside window: %+v err=%v", chunks, err)
	}
}

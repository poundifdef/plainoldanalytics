package webapp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jaygoel.com/plainoldanalytics/adapters/capture"
	"jaygoel.com/plainoldanalytics/storage"
	"jaygoel.com/plainoldanalytics/storage/unimplemented_store"
)

type fakeStorage struct {
	unimplemented_store.Unimplemented
	traffic []storage.Traffic
	events  []storage.Event
	replays []storage.ReplayChunk
}

func (f *fakeStorage) Traffic(ctx context.Context, filter storage.TrafficFilter) ([]storage.Traffic, error) {
	var out []storage.Traffic
	for _, t := range f.traffic {
		if filter.Visitor != 0 && t.Visitor != filter.Visitor {
			continue
		}
		if filter.ContextKey != "" && t.Context[filter.ContextKey] != filter.ContextValue {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func (f *fakeStorage) TrafficByID(ctx context.Context, id uint64) (storage.Traffic, error) {
	for _, t := range f.traffic {
		if t.ID == id {
			return t, nil
		}
	}
	return storage.Traffic{}, storage.ErrNotFound
}

func (f *fakeStorage) TrackEvent(ctx context.Context, e storage.Event) error {
	f.events = append(f.events, e)
	return nil
}

func (f *fakeStorage) Events(ctx context.Context, filter storage.EventFilter) ([]storage.Event, error) {
	if filter.Visitor == 0 {
		return f.events, nil
	}
	var out []storage.Event
	for _, e := range f.events {
		if e.Visitor == filter.Visitor {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeStorage) SaveReplay(ctx context.Context, c storage.ReplayChunk) error {
	f.replays = append(f.replays, c)
	return nil
}

func (f *fakeStorage) TrafficStats(ctx context.Context, since time.Time, userKey string) (storage.TrafficStats, error) {
	return storage.TrafficStats{
		Views: 3, Visitors: 2,
		Pages: []storage.NameCount{{Name: "/b/{c}", Count: 2}, {Name: "/", Count: 1}},
		Referrers: []storage.NameCount{
			{Name: "https://google.com/search", Count: 2},
			{Name: "https://example.com/self", Count: 1}, // same-site: dropped
		},
		UserAgents: []storage.NameCount{{
			Name:  "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
			Count: 2,
		}},
		Users: []storage.NameCount{{Name: "jay@example.net", Count: 2}},
	}, nil
}

var sessStart = time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)

func (f *fakeStorage) Sessions(ctx context.Context, filter storage.SessionFilter) ([]storage.Session, error) {
	all := []storage.Session{{
		Visitor: 5551, Start: sessStart, End: sessStart.Add(time.Minute),
		Annotation: "jay", HasReplay: true,
	}}
	var out []storage.Session
	for _, s := range all {
		if filter.Visitor != 0 && s.Visitor != filter.Visitor {
			continue
		}
		if filter.ContextValue != "" && s.Annotation != filter.ContextValue {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

func newTestMux(store storage.Storage) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/analytics/", http.StripPrefix("/analytics", New(capture.New(store, storage.NewConfig())).Handler()))
	return mux
}

func get(t *testing.T, mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return w
}

func TestPages(t *testing.T) {
	store := &fakeStorage{
		traffic: []storage.Traffic{{
			ID: 123456789, Method: "GET", Pattern: "/b/{c}", Status: 200, IP: "1.2.3.4",
			Visitor: 5551,
			Headers: map[string][]string{"X-Custom": {"one", "two"}},
			Context: map[string]string{"user": "jay"},
		}},
		events: []storage.Event{{ID: 1, Name: "signup", Visitor: 5551}},
	}
	mux := newTestMux(store)

	sessionPath := fmt.Sprintf("/analytics/sessions/5551/%d", sessStart.UnixMicro())
	for path, want := range map[string]string{
		sessionPath + "/replay":                        "rrweb-player.min.js",
		"/analytics/":                                  "Overview",
		"/analytics/traffic":                           `href="traffic/123456789"`,
		"/analytics/traffic/123456789":                 "X-Custom",
		"/analytics/events":                            "Events",
		"/analytics/sessions":                          `href="sessions/5551/`,
		sessionPath:                                    "jay",
		"/analytics/visitors/5551":                     "jay",
		"/analytics/users/jay":                         "5551",
		"/analytics/settings":                          "Frontend snippet",
		"/analytics/static/style.css":                  "table",
		"/analytics/e.js":                              "poa.init_session_recording",
		"/analytics/static/vendor/rrweb.min.js":        "rrweb",
		"/analytics/static/vendor/rrweb-player.min.js": "rrwebPlayer",
		"/analytics/static/vendor/rrweb.css":           "replayer-wrapper",
	} {
		w := get(t, mux, path)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d, body: %s", path, w.Code, w.Body)
		}
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("%s: body missing %q:\n%s", path, want, w.Body)
		}
	}

	// The traffic list stays simple: no header dumps in the table itself.
	// Every non-identity cell in a row links straight to the permalink page,
	// which shows everything about that request — query params, path
	// params, context, and headers — in one click, no intermediate panel.
	{
		body := get(t, mux, "/analytics/traffic").Body.String()
		if strings.Contains(body, "X-Custom") {
			t.Error("traffic list should not include headers")
		}
		if !strings.Contains(body, `href="traffic/123456789">GET</a>`) {
			t.Errorf("traffic list rows should link straight to the request's permalink page:\n%s", body)
		}
		// The Visitor and User cells are separate, direct links to their
		// own pages, distinct from the row's request-detail link.
		if !strings.Contains(body, `href="visitors/5551">5551</a>`) {
			t.Errorf("traffic list's Visitor cell should link straight to the visitor page:\n%s", body)
		}
		if !strings.Contains(body, `href="users/jay">jay</a>`) {
			t.Errorf("traffic list's User cell should link straight to the user page:\n%s", body)
		}
	}
	if body := get(t, mux, "/analytics/traffic/123456789").Body.String(); !strings.Contains(body, `href="users/jay">jay</a>`) {
		t.Error("request permalink page should link the known user to the user page, not just the visitor")
	}
	// Events don't carry context like Traffic does, so the user has to be
	// resolved from the visitor's sessions — the same identity used
	// everywhere else, not a raw visitor number.
	if body := get(t, mux, "/analytics/events").Body.String(); !strings.Contains(body, `href="users/jay">jay</a>`) {
		t.Errorf("events page should resolve the visitor to its known user:\n%s", body)
	}
	// The user page aggregates every visitor sharing that identity: it must
	// list visitor 5551 and link back to it, distinct from the visitor page.
	if body := get(t, mux, "/analytics/users/jay").Body.String(); !strings.Contains(body, `href="visitors/5551">5551</a>`) {
		t.Errorf("user page should list and link to its visitors:\n%s", body)
	}
	// The visitor page links back up to the user, closing the loop.
	if body := get(t, mux, "/analytics/visitors/5551").Body.String(); !strings.Contains(body, `href="users/jay">jay</a>`) {
		t.Errorf("visitor page should link back to its known user:\n%s", body)
	}
	if w := get(t, mux, "/analytics/traffic/999"); w.Code != http.StatusNotFound {
		t.Errorf("unknown traffic: got %d, want 404", w.Code)
	}
	if w := get(t, mux, "/analytics/visitors/999999"); w.Code != http.StatusNotFound {
		t.Errorf("unknown visitor: got %d, want 404", w.Code)
	}
	if w := get(t, mux, "/analytics/users/nobody"); w.Code != http.StatusNotFound {
		t.Errorf("unknown user: got %d, want 404", w.Code)
	}
}

func TestDashboardStats(t *testing.T) {
	mux := newTestMux(&fakeStorage{})
	body := get(t, mux, "/analytics/").Body.String()

	// Breakdown cards: pages, referrer host, browser/OS/device from the UA,
	// and the user drill-down link.
	for _, want := range []string{
		"Pages", "/b/{c}", "google.com", "Chrome", "macOS", "Desktop",
		"traffic?pattern=%2Fb%2F%7Bc%7D",
		"users/jay@example.net",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	// Same-site referrals (httptest requests carry Host example.com) are
	// dropped rather than listed as a referrer.
	if strings.Contains(body, "example.com/self") || strings.Contains(body, ">example.com<") {
		t.Error("dashboard should drop same-site referrers")
	}

	// Range switcher: unknown ranges fall back to 24h, known ones stick.
	if body := get(t, mux, "/analytics/?range=7d").Body.String(); !strings.Contains(body, "Requests &middot; 7 days") {
		t.Error("range switcher: 7d not applied")
	}
	if body := get(t, mux, "/analytics/?range=bogus").Body.String(); !strings.Contains(body, "Requests &middot; 24 hours") {
		t.Error("range switcher: bogus range should fall back to 24h")
	}
}

func TestTrackEvent(t *testing.T) {
	store := &fakeStorage{}
	mux := newTestMux(store)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/analytics/e",
		strings.NewReader(`{"name":"button_click","props":{"where":"demo"},"visitor":"5551","path":"/"}`))
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if len(store.events) != 1 || store.events[0].Name != "button_click" || store.events[0].Visitor != 5551 {
		t.Errorf("stored events: %+v", store.events)
	}

	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/analytics/e", strings.NewReader(`{}`)))
	if w.Code != http.StatusBadRequest {
		t.Errorf("nameless event: got %d, want 400", w.Code)
	}
}

func TestSaveReplayAndReplayJSON(t *testing.T) {
	store := &fakeStorage{}
	mux := newTestMux(store)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/analytics/replay",
		strings.NewReader(`{"visitor":"5551","events":[{"type":4},{"type":2}]}`))
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status %d: %s", w.Code, w.Body)
	}
	if len(store.replays) != 1 || store.replays[0].Visitor != 5551 {
		t.Fatalf("stored replays: %+v", store.replays)
	}
}

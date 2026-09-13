// Package webapp is the analytics dashboard: a self-contained web app over
// a Capturer that host applications mount under any prefix. It serves the
// UI (dashboard, traffic, events, sessions, replay), the tracking snippet
// e.js, and the ingestion endpoints POST /e (custom events) and POST
// /replay (rrweb chunks). Templates and assets, including the rrweb player,
// are embedded, so importing binaries are fully self-contained.
package webapp

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"jaygoel.com/plainoldanalytics/adapters/capture"
	"jaygoel.com/plainoldanalytics/storage"
)

//go:embed templates
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

// App is the analytics web app: a self-contained application over a Storage
// that host applications mount wherever they like. It only depends on the
// Storage interface, so it works with any backend. Built on net/http and
// html/template; templates and assets are embedded, so the compiled binary
// is self-contained.
type App struct {
	store   storage.Storage
	mux     *http.ServeMux
	pages   map[string]*template.Template
	userKey string
	config  storage.Config
}

// New builds the app over a Capturer, reusing its storage and its
// storage.Config so the dashboard and the middleware always agree on
// configuration.
func New(cap *capture.Capturer) *App {
	a := &App{
		store:   cap.Storage,
		userKey: cap.Config.UserKey,
		config:  cap.Config,
		mux:     http.NewServeMux(),
		// Each page is parsed together with the shared layout so pages can
		// override the layout's blocks without clashing with each other.
		pages: map[string]*template.Template{
			"dashboard": parsePage("dashboard.html"),
			"traffic":   parsePage("traffic.html"),
			"request":   parsePage("request.html"),
			"events":    parsePage("events.html"),
			"sessions":  parsePage("sessions.html"),
			"session":   parsePage("session.html"),
			"replay":    parsePage("replay.html"),
			"visitor":   parsePage("visitor.html"),
			"user":      parsePage("user.html"),
			"settings":  parsePage("settings.html"),
		},
	}
	a.mux.HandleFunc("GET /{$}", a.dashboardPage)
	a.mux.HandleFunc("GET /traffic", a.trafficPage)
	a.mux.HandleFunc("GET /traffic/{id}", a.requestPage)
	a.mux.HandleFunc("GET /events", a.eventsPage)
	a.mux.HandleFunc("GET /sessions", a.sessionsPage)
	a.mux.HandleFunc("GET /sessions/{visitor}/{start}", a.sessionPage)
	a.mux.HandleFunc("GET /sessions/{visitor}/{start}/replay", a.replayPage)
	a.mux.HandleFunc("GET /sessions/{visitor}/{start}/replay.json", a.replayJSON)
	a.mux.HandleFunc("GET /visitors/{id}", a.visitorPage)
	a.mux.HandleFunc("GET /users/{name}", a.userPage)
	a.mux.HandleFunc("GET /settings", a.settingsPage)
	a.mux.HandleFunc("POST /e", a.trackEvent)
	a.mux.HandleFunc("POST /replay", a.saveReplay)
	a.mux.Handle("GET /static/", http.FileServerFS(staticFS))
	a.mux.HandleFunc("GET /e.js", a.snippet)
	return a
}

var tmplFuncs = template.FuncMap{
	"mod": func(a, b int) int { return a % b },
}

func parsePage(page string) *template.Template {
	return template.Must(template.New("layout.html").Funcs(tmplFuncs).
		ParseFS(templateFS, "templates/layout.html", "templates/"+page))
}

// Handler returns the app as a standard http.Handler so it can be bolted
// onto any net/http application, e.g.:
//
//	mux.Handle("/analytics/", http.StripPrefix("/analytics", app.Handler()))
//
// The app uses only relative URLs internally (anchored by a per-request
// <base> tag), so it works under any mount prefix.
func (a *App) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.Exclude(r.Context())
		a.mux.ServeHTTP(w, r)
	})
}

// render writes the named page wrapped in the shared layout. Root, the app's
// mount prefix, is injected for the layout's <base> tag so relative URLs
// resolve against the mount root on pages at any depth.
func (a *App) render(w http.ResponseWriter, r *http.Request, page string, data map[string]any) {
	if data == nil {
		data = map[string]any{}
	}
	data["Root"] = mountRoot(r)
	data["UserKey"] = a.userKey
	data["RangeParam"] = rangeParam(r)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.pages[page].ExecuteTemplate(w, "layout", data); err != nil {
		slog.Error("webapp render", "page", page, "error", err)
	}
}

// parseID parses a decimal ID from its transport form (URL path segment,
// query param, JSON string field — all text); 0 means absent/invalid.
// Session and other IDs are integers everywhere server-side; only the wire
// carries them as strings, since JavaScript numbers lose precision past
// 2^53.
func parseID(s string) uint64 {
	v, _ := strconv.ParseUint(s, 10, 64)
	return v
}

// mountRoot returns the app's mount prefix with a trailing slash (e.g.
// "/analytics/"). Mounting helpers like http.StripPrefix rewrite r.URL.Path
// but leave r.RequestURI untouched, so the prefix is their difference.
func mountRoot(r *http.Request) string {
	orig := r.RequestURI
	if i := strings.IndexByte(orig, '?'); i >= 0 {
		orig = orig[:i]
	}
	return strings.TrimSuffix(orig, r.URL.EscapedPath()) + "/"
}

// contextFilter resolves the filter form's context inputs: the "user"
// shortcut field maps onto the configured user context key unless an
// explicit key/value pair was given.
func (a *App) contextFilter(r *http.Request) (key, value string) {
	key, value = r.FormValue("key"), r.FormValue("value")
	if u := r.FormValue("user"); u != "" && key == "" {
		key, value = a.userKey, u
	}
	return key, value
}

func (a *App) trafficPage(w http.ResponseWriter, r *http.Request) {
	key, value := a.contextFilter(r)
	f := storage.TrafficFilter{
		Pattern:      r.FormValue("pattern"),
		Visitor:      parseID(r.FormValue("visitor")),
		ContextKey:   key,
		ContextValue: value,
	}
	f.Status, _ = strconv.Atoi(r.FormValue("status"))
	traffic, err := a.store.Traffic(r.Context(), f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	a.render(w, r, "traffic", map[string]any{
		"Nav": "traffic", "Crumb": trafficCrumb(f, r.FormValue("user")),
		"Rows": traffic, "Filter": f, "UserFilter": r.FormValue("user"),
	})
}

// trafficCrumb summarizes the active traffic filters for the top-bar
// breadcrumb; an empty result means no filters are set.
func trafficCrumb(f storage.TrafficFilter, user string) string {
	var parts []string
	if f.Pattern != "" {
		parts = append(parts, "pattern="+f.Pattern)
	}
	if f.Status != 0 {
		parts = append(parts, "status="+strconv.Itoa(f.Status))
	}
	if f.Visitor != 0 {
		parts = append(parts, "visitor="+strconv.FormatUint(f.Visitor, 10))
	}
	if user != "" {
		parts = append(parts, "user="+user)
	} else if f.ContextKey != "" {
		parts = append(parts, f.ContextKey+"="+f.ContextValue)
	}
	return strings.Join(parts, "  ·  ")
}

func (a *App) requestPage(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseUint(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	t, err := a.store.TrafficByID(r.Context(), id)
	if errors.Is(err, storage.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, "request", map[string]any{"Nav": "traffic", "T": t})
}

// eventRow pairs an Event with its visitor's resolved user, if known.
// Events don't carry context (unlike Traffic), so the user has to be
// resolved from the visitor's sessions instead — the same annotation
// mechanism every other page uses.
type eventRow struct {
	storage.Event
	User string
}

func (a *App) eventsPage(w http.ResponseWriter, r *http.Request) {
	visitor := parseID(r.FormValue("visitor"))
	all, err := a.store.Events(r.Context(), storage.EventFilter{Visitor: visitor})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	users, err := a.visitorUsers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	counts := map[string]int{}
	for _, e := range all {
		counts[e.Name]++
	}
	names := mapCounts(counts)

	name := r.FormValue("name")
	var events []eventRow
	for _, e := range all {
		if name == "" || e.Name == name {
			events = append(events, eventRow{Event: e, User: users[e.Visitor]})
		}
	}

	a.render(w, r, "events", map[string]any{
		"Nav":   "events",
		"Names": names, "Selected": name,
		"Events": events, "Total": len(all),
		"Filter": storage.EventFilter{Name: name, Visitor: visitor},
	})
}

// visitorUsers maps every visitor with a known identity to their user, via
// the same session-annotation lookup the sessions and visitor pages use.
// Visitors absent from the map are anonymous.
func (a *App) visitorUsers(ctx context.Context) (map[uint64]string, error) {
	sessions, err := a.store.Sessions(ctx, storage.SessionFilter{AnnotateKey: a.userKey})
	if err != nil {
		return nil, err
	}
	users := make(map[uint64]string, len(sessions))
	for _, s := range sessions {
		if s.Annotation != "" {
			users[s.Visitor] = s.Annotation
		}
	}
	return users, nil
}

func (a *App) sessionsPage(w http.ResponseWriter, r *http.Request) {
	key, value := a.contextFilter(r)
	f := storage.SessionFilter{
		Visitor:      parseID(r.FormValue("visitor")),
		AnnotateKey:  a.userKey,
		ContextKey:   key,
		ContextValue: value,
		EventName:    r.FormValue("event"),
	}
	sessions, err := a.store.Sessions(r.Context(), f)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tab := r.FormValue("tab")
	var rows []storage.Session
	for _, s := range sessions {
		switch tab {
		case "replay":
			if !s.HasReplay {
				continue
			}
		case "events":
			if s.EventCount == 0 {
				continue
			}
		}
		rows = append(rows, s)
	}

	a.render(w, r, "sessions", map[string]any{
		"Nav":      "sessions",
		"Sessions": rows, "Total": len(sessions), "Tab": tab,
		"TabParam": queryWithout(r, "tab"),
		"Filter":   f, "UserFilter": r.FormValue("user"),
	})
}

func (a *App) sessionPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session, ok := a.findSession(w, r)
	if !ok {
		return
	}
	events, err := a.store.Events(ctx, storage.EventFilter{
		Visitor: session.Visitor, From: session.Start, To: session.End,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	traffic, err := a.store.Traffic(ctx, storage.TrafficFilter{
		Visitor: session.Visitor, From: session.Start, To: session.End,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, "session", map[string]any{
		"Nav": "sessions", "Crumb": sessionCrumb(session),
		"S": session, "Events": events, "Traffic": traffic,
	})
}

// sessionCrumb is the top-bar breadcrumb for a single session: its user if
// known, otherwise a shortened visitor ID.
func sessionCrumb(s storage.Session) string {
	if s.Annotation != "" {
		return s.Annotation
	}
	return "visitor " + strconv.FormatUint(s.Visitor, 10)
}

// findSession resolves the {visitor}/{start} path segments to a derived
// session; start is the session's start time in Unix microseconds, which
// addresses it stably since sessions are not stored. On failure it writes
// the error response and returns ok=false.
func (a *App) findSession(w http.ResponseWriter, r *http.Request) (storage.Session, bool) {
	visitor := parseID(r.PathValue("visitor"))
	start := parseID(r.PathValue("start"))
	if visitor == 0 || start == 0 {
		http.NotFound(w, r)
		return storage.Session{}, false
	}
	sessions, err := a.store.Sessions(r.Context(),
		storage.SessionFilter{Visitor: visitor, AnnotateKey: a.userKey})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return storage.Session{}, false
	}
	for _, s := range sessions {
		if uint64(s.Start.UnixMicro()) == start {
			return s, true
		}
	}
	http.NotFound(w, r)
	return storage.Session{}, false
}

// replayPage is the dedicated full-width player page for one session. Its
// sidebar timeline merges the session's requests and events so the viewer
// can jump the (JS) rrweb player to a moment described by either.
func (a *App) replayPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	session, ok := a.findSession(w, r)
	if !ok {
		return
	}
	events, err := a.store.Events(ctx, storage.EventFilter{
		Visitor: session.Visitor, From: session.Start, To: session.End,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	traffic, err := a.store.Traffic(ctx, storage.TrafficFilter{
		Visitor: session.Visitor, From: session.Start, To: session.End,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a.render(w, r, "replay", map[string]any{
		"Nav": "sessions", "Crumb": sessionCrumb(session),
		"S": session, "Timeline": buildTimeline(traffic, events),
	})
}

// replayJSON returns a derived session's rrweb events: all chunks in its
// window concatenated into a single JSON array for the player.
func (a *App) replayJSON(w http.ResponseWriter, r *http.Request) {
	session, ok := a.findSession(w, r)
	if !ok {
		return
	}
	chunks, err := a.store.Replay(r.Context(), session.Visitor, session.Start, session.End)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var events []json.RawMessage
	for _, c := range chunks {
		var chunk []json.RawMessage
		if err := json.Unmarshal(c.Data, &chunk); err != nil {
			continue
		}
		events = append(events, chunk...)
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(events); err != nil {
		slog.Error("webapp encode replay", "error", err)
	}
}

func (a *App) trackEvent(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Name    string         `json:"name"`
		Props   map[string]any `json:"props"`
		Visitor string         `json:"visitor"`
		Session string         `json:"session"`
		Path    string         `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if payload.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	err := a.store.TrackEvent(r.Context(), storage.Event{
		Name:    payload.Name,
		Props:   payload.Props,
		Visitor: parseID(payload.Visitor),
		Session: parseID(payload.Session),
		Path:    payload.Path,
		IP:      storage.ClientIP(r),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *App) saveReplay(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Visitor string            `json:"visitor"`
		Session string            `json:"session"`
		Events  []json.RawMessage `json:"events"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<20)).Decode(&payload); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	visitor := parseID(payload.Visitor)
	if visitor == 0 || len(payload.Events) == 0 {
		http.Error(w, "visitor and events are required", http.StatusBadRequest)
		return
	}
	data, err := json.Marshal(payload.Events)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	err = a.store.SaveReplay(r.Context(), storage.ReplayChunk{
		Visitor: visitor,
		Session: parseID(payload.Session),
		Data:    data,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// snippet serves the tracking snippet with an explicit content type; going
// through the generic file server would work too, but this route documents
// that e.js is part of the public API surface.
func (a *App) snippet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	data, err := staticFS.ReadFile("static/e.js")
	if err != nil {
		http.Error(w, "snippet missing", http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, "%s", data)
}

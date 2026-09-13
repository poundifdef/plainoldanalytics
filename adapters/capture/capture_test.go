package capture

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"jaygoel.com/plainoldanalytics/storage"
	"jaygoel.com/plainoldanalytics/storage/unimplemented_store"
)

type recordingStorage struct {
	unimplemented_store.Unimplemented
	captures []storage.Capture
}

func (r *recordingStorage) Record(_ *http.Request, c storage.Capture) {
	r.captures = append(r.captures, c)
}

type ctxKey string

const tenantKey ctxKey = "tenant"

func TestSetFromHandler(t *testing.T) {
	store := &recordingStorage{}
	cp := New(store, storage.NewConfig())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Set(r.Context(), "user", "jay@example.com")
		Set(r.Context(), tenantKey, "acme") // typed keys record under their string form
		w.Write([]byte("ok"))
	})

	cp.Serve(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), "/", handler)

	if len(store.captures) != 1 {
		t.Fatalf("captures: %d", len(store.captures))
	}
	got := store.captures[0].Context
	want := map[string]string{"user": "jay@example.com", "tenant": "acme"}
	if len(got) != len(want) || got["user"] != want["user"] || got["tenant"] != want["tenant"] {
		t.Errorf("context: got %v, want %v", got, want)
	}
}

func TestLaterSetWins(t *testing.T) {
	store := &recordingStorage{}
	cp := New(store, storage.NewConfig())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Set(r.Context(), "user", "first")
		Set(r.Context(), "user", "second")
	})

	cp.Serve(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), "/", handler)

	if got := store.captures[0].Context["user"]; got != "second" {
		t.Errorf("user: got %q, want second (later Set should win)", got)
	}
}

func TestBrowserSessionCookieGroupsRequests(t *testing.T) {
	store := &recordingStorage{}
	cp := New(store, storage.NewConfig())
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})

	first := httptest.NewRecorder()
	cp.Serve(first, httptest.NewRequest("GET", "/", nil), "/", h)
	var session *http.Cookie
	for _, c := range first.Result().Cookies() {
		if c.Name == "poa_session" {
			session = c
		}
	}
	if session == nil || session.MaxAge != 0 || !session.Expires.IsZero() {
		t.Fatalf("session cookie = %+v; want session-only cookie", session)
	}

	secondReq := httptest.NewRequest("GET", "/", nil)
	secondReq.AddCookie(session)
	cp.Serve(httptest.NewRecorder(), secondReq, "/", h)
	if len(store.captures) != 2 || store.captures[0].Session == 0 ||
		store.captures[0].Session != store.captures[1].Session {
		t.Fatalf("sessions = %+v", store.captures)
	}
}

func TestPlainContextValuesNotRecorded(t *testing.T) {
	store := &recordingStorage{}
	cp := New(store, storage.NewConfig())

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	// A value placed on the request context directly (the old passive way)
	// is not harvested; only Set records values.
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), tenantKey, "acme"))
	cp.Serve(httptest.NewRecorder(), req, "/", handler)

	if got := store.captures[0].Context; got != nil {
		t.Errorf("context: got %v, want nil", got)
	}
}

func TestSetOutsideMiddlewareIsNoop(t *testing.T) {
	Set(context.Background(), "user", "jay") // must not panic
}

func TestExclude(t *testing.T) {
	Exclude(context.Background()) // safe outside analytics middleware
	store := &recordingStorage{}
	cp := New(store, storage.NewConfig())
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Set(r.Context(), "user", "jay")
		if r.URL.Path == "/health" {
			ctx := context.WithValue(r.Context(), tenantKey, "acme")
			Exclude(ctx)
		}
		w.Write([]byte("ok"))
	})
	for _, path := range []string{"/health", "/home"} {
		w := httptest.NewRecorder()
		cp.Serve(w, httptest.NewRequest("GET", path, nil), path, handler)
		if w.Body.String() != "ok" {
			t.Fatalf("%s: handler did not complete", path)
		}
	}
	if len(store.captures) != 1 || store.captures[0].Pattern != "/home" {
		t.Fatalf("want only /home recorded, got %+v", store.captures)
	}
}

func TestSetWithDerivedContextAndRequestIsolation(t *testing.T) {
	store := &recordingStorage{}
	cp := New(store, storage.NewConfig())
	var saved context.Context
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Set(r.Context(), "tenant", "acme")
		r = r.WithContext(context.WithValue(r.Context(), tenantKey, "ordinary value"))
		Set(r.Context(), "items_count", 3)
		saved = r.Context()
	})
	cp.Serve(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), "/", handler)
	Set(saved, "tenant", "too late")
	cp.Serve(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), "/",
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	got := store.captures[0].Context
	if len(got) != 2 || got["tenant"] != "acme" || got["items_count"] != "3" {
		t.Errorf("attributes: got %v", got)
	}
	if got := store.captures[1].Context; got != nil {
		t.Errorf("attributes leaked into next request: %v", got)
	}
}

func TestConcurrentSet(t *testing.T) {
	store := &recordingStorage{}
	cp := New(store, storage.NewConfig())
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var wg sync.WaitGroup
		for i := range 20 {
			wg.Go(func() { Set(r.Context(), strconv.Itoa(i), i) })
		}
		wg.Wait()
	})
	cp.Serve(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), "/", handler)
	got := store.captures[0].Context
	if len(got) != 20 {
		t.Fatalf("got %d attributes, want 20", len(got))
	}
	for i := range 20 {
		key := strconv.Itoa(i)
		if got[key] != key {
			t.Errorf("attribute %s: got %q", key, got[key])
		}
	}
}

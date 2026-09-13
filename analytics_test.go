package plainoldanalytics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"jaygoel.com/plainoldanalytics"
	"jaygoel.com/plainoldanalytics/storage"
	"jaygoel.com/plainoldanalytics/storage/memory_store"
)

func TestMiddleware(t *testing.T) {
	store, err := memory_store.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	analytics := plainoldanalytics.New(store)
	router := http.NewServeMux()
	router.HandleFunc("GET /hello/{name}", func(w http.ResponseWriter, r *http.Request) {
		plainoldanalytics.Set(r.Context(), "plan", "pro")
		w.Write([]byte("hello " + r.PathValue("name")))
	})
	router.HandleFunc("/reports/", analytics.Handler())
	router.HandleFunc("/admin/stats/", analytics.Handler())
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		plainoldanalytics.Exclude(r.Context())
		w.Write([]byte("ok"))
	})
	loggedRouter := analytics.Middleware(router)
	w := httptest.NewRecorder()
	loggedRouter.ServeHTTP(w, httptest.NewRequest("GET", "/hello/jay", nil))
	if w.Code != http.StatusOK || w.Body.String() != "hello jay" {
		t.Fatalf("response: %d %q", w.Code, w.Body.String())
	}
	for _, path := range []string{"/reports/", "/reports/e.js", "/reports/traffic", "/reports/static/style.css", "/admin/stats/", "/admin/stats/e.js", "/health"} {
		w := httptest.NewRecorder()
		loggedRouter.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, w.Code)
		}
		if strings.HasSuffix(path, "/") && !strings.Contains(w.Body.String(), `<base href="`+path+`">`) {
			t.Errorf("%s: dashboard links use the wrong mount prefix", path)
		}
	}
	for path, body := range map[string]string{
		"/reports/e":      `{"name":"signup","props":{"plan":"pro"},"visitor":"123"}`,
		"/reports/replay": `{"visitor":"123","events":[{"type":2}]}`,
	} {
		w := httptest.NewRecorder()
		loggedRouter.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(body)))
		if w.Code >= 300 {
			t.Fatalf("%s: status %d, body %s", path, w.Code, w.Body)
		}
	}
	events, err := store.Events(t.Context(), storage.EventFilter{})
	if err != nil || len(events) != 1 || events[0].Name != "signup" {
		t.Fatalf("events: %+v, error: %v", events, err)
	}
	replays, err := store.Replay(t.Context(), 123, time.Time{}, time.Time{})
	if err != nil || len(replays) != 1 {
		t.Fatalf("replays: %+v, error: %v", replays, err)
	}
	traffic, err := store.Traffic(t.Context(), storage.TrafficFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(traffic) != 1 {
		t.Fatalf("got %d records, want only the application request", len(traffic))
	}
	if traffic[0].Pattern != "/hello/{name}" || traffic[0].Context["plan"] != "pro" {
		t.Fatalf("capture: %+v", traffic[0])
	}
}

func TestMount(t *testing.T) {
	for _, path := range []string{"/reports", "/reports/", "/admin/stats", "/"} {
		t.Run(path, func(t *testing.T) {
			store, err := memory_store.New()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { store.Close() })
			analytics := plainoldanalytics.New(store)
			router := http.NewServeMux()
			router.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("hello"))
			})
			analytics.Mount(router, path)
			logged := analytics.Middleware(router)
			prefix := strings.TrimSuffix(path, "/")
			if prefix != "" {
				w := httptest.NewRecorder()
				logged.ServeHTTP(w, httptest.NewRequest("GET", prefix+"?window=7d", nil))
				if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != prefix+"/?window=7d" {
					t.Fatalf("redirect: %d %q", w.Code, w.Header().Get("Location"))
				}
			}
			for _, suffix := range []string{"/", "/e.js", "/static/style.css", "/traffic"} {
				w := httptest.NewRecorder()
				logged.ServeHTTP(w, httptest.NewRequest("GET", prefix+suffix, nil))
				if w.Code != http.StatusOK {
					t.Fatalf("%s: status %d", prefix+suffix, w.Code)
				}
				if suffix == "/" && !strings.Contains(w.Body.String(), `<base href="`+prefix+`/">`) {
					t.Fatal("incorrect dashboard base URL")
				}
			}
			logged.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/hello", nil))
			traffic, err := store.Traffic(t.Context(), storage.TrafficFilter{})
			if err != nil || len(traffic) != 1 || traffic[0].Pattern != "/hello" {
				t.Fatalf("want only /hello recorded, got %+v, error: %v", traffic, err)
			}
		})
	}
}

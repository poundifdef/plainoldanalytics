package chi

import (
	"context"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"jaygoel.com/plainoldanalytics/adapters/capture"
	"jaygoel.com/plainoldanalytics/storage"
	"jaygoel.com/plainoldanalytics/storage/memory_store"
)

func TestMiddleware(t *testing.T) {
	store, err := memory_store.New()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	router := chi.NewRouter()
	router.Use(New(capture.New(store, storage.NewConfig())).Wrap)
	router.Get("/b/{c}", func(w http.ResponseWriter, r *http.Request) {
		capture.Set(r.Context(), "plan", "pro")
		w.Write([]byte("b"))
	})
	router.Get("/f/*", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("f")) })

	tests := []struct {
		path    string
		pattern string
		values  map[string]string
	}{
		{"/b/x", "/b/{c}", map[string]string{"c": "x"}},
		{"/f/a/b", "/f/{wildcard...}", map[string]string{"wildcard": "a/b"}},
	}
	for _, tt := range tests {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		router.ServeHTTP(w, req)
		if cookie := w.Header().Get("Set-Cookie"); cookie == "" {
			t.Errorf("%s: no session cookie set", tt.path)
		}
	}

	traffic, err := store.Traffic(context.Background(), storage.TrafficFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(traffic) != len(tests) {
		t.Fatalf("got %d traffic rows, want %d", len(traffic), len(tests))
	}
	for _, tt := range tests {
		var e *storage.Traffic
		for i := range traffic {
			if traffic[i].Pattern == tt.pattern {
				e = &traffic[i]
				break
			}
		}
		if e == nil {
			t.Errorf("%s: no traffic with pattern %q", tt.path, tt.pattern)
			continue
		}
		if e.Method != http.MethodGet {
			t.Errorf("%s: method = %q, want %q", tt.path, e.Method, http.MethodGet)
		}
		if tt.pattern == "/b/{c}" && e.Context["plan"] != "pro" {
			t.Errorf("attributes: got %v, want plan=pro", e.Context)
		}
		if !maps.Equal(e.PathParams, tt.values) {
			t.Errorf("%s: path params = %v, want %v", tt.path, e.PathParams, tt.values)
		}
		if e.Status != http.StatusOK {
			t.Errorf("%s: status = %d, want %d", tt.path, e.Status, http.StatusOK)
		}
		if e.Size != 1 {
			t.Errorf("%s: size = %d, want 1", tt.path, e.Size)
		}
		if e.Visitor == 0 {
			t.Errorf("%s: no visitor recorded", tt.path)
		}
	}
}

// TestGroupScopedMiddleware verifies the README's mounting pattern: routes
// registered outside the middleware's group (like the dashboard) are not
// recorded, while group routes are.
func TestGroupScopedMiddleware(t *testing.T) {
	store, err := memory_store.New()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	router := chi.NewRouter()
	router.Get("/analytics/dash", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("dash")) })

	router.Group(func(r chi.Router) {
		r.Use(New(capture.New(store, storage.NewConfig())).Wrap)
		r.Get("/hello/{name}", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("hi")) })
	})

	for _, path := range []string{"/analytics/dash", "/hello/x"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, w.Code)
		}
	}

	traffic, err := store.Traffic(context.Background(), storage.TrafficFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(traffic) != 1 || traffic[0].Pattern != "/hello/{name}" {
		t.Errorf("want only /hello/{name} recorded, got %+v", traffic)
	}
}

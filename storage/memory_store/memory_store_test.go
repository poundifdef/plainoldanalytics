package memory_store_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"jaygoel.com/plainoldanalytics/storage/memory_store"
)

func TestPlainOldAnalytics(t *testing.T) {
	router := http.NewServeMux()
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	})
	analytics := memory_store.PlainOldAnalytics()
	loggedRouter := analytics.Middleware(router)
	w := httptest.NewRecorder()
	loggedRouter.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusOK || w.Body.String() != "hello" {
		t.Fatalf("response: %d %q", w.Code, w.Body.String())
	}
	if len(w.Result().Cookies()) == 0 {
		t.Fatal("analytics visitor cookie missing")
	}
	// Obtaining a handler must not reserve any routes or change the middleware.
	_ = analytics.Handler()
	for _, path := range []string{"/analytics", "/analytics/", "/analytics/e.js", "/analytics-other"} {
		w := httptest.NewRecorder()
		loggedRouter.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK || w.Body.String() != "hello" {
			t.Fatalf("unmounted %s: %d %q", path, w.Code, w.Body.String())
		}
	}
}

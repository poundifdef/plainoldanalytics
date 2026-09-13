package chi_test

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"jaygoel.com/plainoldanalytics/adapters/capture"
	chiadapter "jaygoel.com/plainoldanalytics/adapters/chi"
	"jaygoel.com/plainoldanalytics/storage"
	"jaygoel.com/plainoldanalytics/storage/unimplemented_store"
)

// Example instruments a chi application. unimplemented_store.Unimplemented
// stands in for a real backend; use storage/duckdb_store (or
// storage/memory_store) in a real application.
func Example() {
	var store storage.Storage = unimplemented_store.Unimplemented{}
	capturer := capture.New(store, storage.NewConfig())

	router := chi.NewRouter()
	router.Use(chiadapter.New(capturer).Wrap)
	router.Get("/hello/{name}", func(w http.ResponseWriter, r *http.Request) {
		capture.Set(r.Context(), "plan", "pro")
		w.Write([]byte("hello " + chi.URLParam(r, "name")))
	})

	log.Fatal(http.ListenAndServe(":8080", router))
}

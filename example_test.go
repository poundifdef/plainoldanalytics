package plainoldanalytics_test

import (
	"log"
	"net/http"

	"jaygoel.com/plainoldanalytics"
	"jaygoel.com/plainoldanalytics/storage"
	"jaygoel.com/plainoldanalytics/storage/unimplemented_store"
)

// ExampleNew wires analytics over any Storage implementation — this package
// knows nothing about any particular backend, so importing it never pulls
// one in. unimplemented_store.Unimplemented stands in here; use a real
// backend package's own PlainOldAnalytics constructor instead (e.g.
// storage/duckdb_store or storage/memory_store) to get both a working
// Storage and this same wiring in one call.
func ExampleNew() {
	var store storage.Storage = unimplemented_store.Unimplemented{}
	analytics := plainoldanalytics.New(store)

	myHandler := func(w http.ResponseWriter, r *http.Request) {
		plainoldanalytics.Set(r.Context(), "user", "user@example.com")
		w.Write([]byte("hello"))
	}
	router := http.NewServeMux()
	router.HandleFunc("/", myHandler)

	analytics.Mount(router, "/analytics")
	loggedRouter := analytics.Middleware(router)

	log.Fatal(http.ListenAndServe(":8080", loggedRouter))
}

package webapp_test

import (
	"log"
	"net/http"

	"jaygoel.com/go/plainoldanalytics/adapters/capture"
	"jaygoel.com/go/plainoldanalytics/storage"
	"jaygoel.com/go/plainoldanalytics/storage/unimplemented_store"
	"jaygoel.com/go/plainoldanalytics/webapp"
)

// ExampleNew mounts the analytics dashboard under /analytics. It is mounted
// as a sibling of the instrumented application handler — not inside it — so
// the dashboard's own routes are not recorded as traffic.
func ExampleNew() {
	var store storage.Storage = unimplemented_store.Unimplemented{}
	capturer := capture.New(store, storage.NewConfig())

	app := http.NewServeMux() // the application being instrumented

	root := http.NewServeMux()
	root.Handle("/analytics/", http.StripPrefix("/analytics", webapp.New(capturer).Handler()))
	root.Handle("/", app)

	log.Fatal(http.ListenAndServe(":8080", root))
}

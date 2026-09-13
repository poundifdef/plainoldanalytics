package duckdb_store_test

import (
	"log"
	"net/http"

	goduckdb "github.com/duckdb/duckdb-go/v2"
	"jaygoel.com/plainoldanalytics/storage/duckdb_store"
)

// ExampleNew opens a persistent analytics database. The caller owns the
// connector: pass "" instead of a filename for an in-memory database, or
// configure DuckDB settings via the connector. Traffic flushes periodically;
// process termination can lose rows buffered since the last successful flush.
func ExampleNew() {
	connector, err := goduckdb.NewConnector("analytics.duckdb", nil)
	if err != nil {
		log.Fatal(err)
	}
	store, err := duckdb_store.New(connector)
	if err != nil {
		log.Fatal(err)
	}
	_ = store // Pass to capture.New to use with a framework adapter.
}

// ExamplePlainOldAnalytics is the batteries-included path: it wraps New with
// the plainoldanalytics.Analytics convenience type (Mount, Middleware, ...)
// in one call, so callers who want DuckDB specifically don't need the root
// package to know about any storage backend at all.
func ExamplePlainOldAnalytics() {
	connector, err := goduckdb.NewConnector("plainoldanalytics.duckdb", nil)
	if err != nil {
		log.Fatal(err)
	}
	analytics, err := duckdb_store.PlainOldAnalytics(connector)
	if err != nil {
		log.Fatal(err)
	}
	defer analytics.Close()

	router := http.NewServeMux()
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello"))
	})

	analytics.Mount(router, "/analytics")
	loggedRouter := analytics.Middleware(router)

	log.Fatal(http.ListenAndServe(":8080", loggedRouter))
}

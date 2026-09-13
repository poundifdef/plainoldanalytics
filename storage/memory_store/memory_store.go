// Package memory_store provides an in-memory Storage. It is a thin wrapper
// that opens an in-memory DuckDB database and records to it via
// storage/duckdb_store, so events are queryable but lost when the store is
// closed or the process exits.
package memory_store

import (
	goduckdb "github.com/duckdb/duckdb-go/v2"
	"jaygoel.com/plainoldanalytics"
	"jaygoel.com/plainoldanalytics/storage/duckdb_store"
)

// New returns a Storage backed by an in-memory DuckDB database. Close releases
// resources early; process-lifetime stores can remain open until exit.
func New() (*duckdb_store.DuckDB, error) {
	connector, err := goduckdb.NewConnector("", nil)
	if err != nil {
		return nil, err
	}
	return duckdb_store.New(connector)
}

// PlainOldAnalytics creates analytics backed by an in-memory database. It
// owns periodic flushing and needs no context or cleanup calls. Data is
// lost on process exit. It panics if the database cannot be initialized —
// an in-memory DuckDB database should never fail to open.
func PlainOldAnalytics() *plainoldanalytics.Analytics {
	store, err := New()
	if err != nil {
		panic(err)
	}
	return plainoldanalytics.New(store)
}

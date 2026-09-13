package duckdb_store

import (
	"github.com/duckdb/duckdb-go/v2"
	"jaygoel.com/plainoldanalytics"
)

// PlainOldAnalytics creates analytics backed by a DuckDB database on the
// given connector. The caller owns the connector — file path vs in-memory,
// and any DuckDB settings — via github.com/duckdb/duckdb-go/v2, e.g.:
//
//	connector, err := duckdb.NewConnector("plainoldanalytics.duckdb", nil)
//	if err != nil {
//		log.Fatal(err)
//	}
//	analytics, err := duckdb_store.PlainOldAnalytics(connector)
//
// Traffic flushes to disk periodically (about once a second); call Close on
// shutdown to flush anything still buffered since the last flush.
func PlainOldAnalytics(connector *duckdb.Connector) (*plainoldanalytics.Analytics, error) {
	store, err := New(connector)
	if err != nil {
		return nil, err
	}
	return plainoldanalytics.New(store), nil
}

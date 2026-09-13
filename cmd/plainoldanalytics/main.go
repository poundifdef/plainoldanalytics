package main

import (
	"log"
	"net/http"

	"github.com/duckdb/duckdb-go/v2"
	"jaygoel.com/plainoldanalytics"
	"jaygoel.com/plainoldanalytics/storage/duckdb_store"
)

func myHandler(w http.ResponseWriter, r *http.Request) {
	plainoldanalytics.Set(r.Context(), "user", "user@example.com")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!doctype html>
<title>Demo app</title>
<h1>Demo app</h1>
<p><a href="/analytics/">Analytics</a></p>
<button onclick="plainoldanalytics.track('button_click', {where: 'demo'})">Track event</button>
<script src="/analytics/e.js"></script>
<script>plainoldanalytics.init_session_recording()</script>`))
}

func main() {
	connector, err := duckdb.NewConnector("plainoldanalytics.duckdb", nil)
	if err != nil {
		log.Fatal(err)
	}
	analytics, err := duckdb_store.PlainOldAnalytics(connector)
	if err != nil {
		log.Fatal(err)
	}

	router := http.NewServeMux()
	router.HandleFunc("/", myHandler)

	analytics.Mount(router, "/analytics")
	loggedRouter := analytics.Middleware(router)

	log.Fatal(http.ListenAndServe(":8080", loggedRouter))
}

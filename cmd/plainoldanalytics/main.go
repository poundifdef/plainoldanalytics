package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"

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
	// Traffic flushes to disk periodically, but Close catches anything still
	// buffered since the last flush — worth doing on a clean shutdown.
	defer analytics.Close()

	router := http.NewServeMux()
	router.HandleFunc("/", myHandler)

	analytics.Mount(router, "/analytics")
	loggedRouter := analytics.Middleware(router)

	server := &http.Server{Addr: ":8080", Handler: loggedRouter}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	log.Print("shutting down")
	if err := server.Shutdown(context.Background()); err != nil {
		log.Print(err)
	}
}

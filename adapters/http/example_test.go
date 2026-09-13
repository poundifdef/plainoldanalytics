package http_test

import (
	"log"
	"net/http"

	"jaygoel.com/plainoldanalytics"
	"jaygoel.com/plainoldanalytics/storage/memory_store"
)

func Example() {
	analytics := memory_store.PlainOldAnalytics()
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

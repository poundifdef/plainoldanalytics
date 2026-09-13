# Plain Old Analytics

Self-hosted, bolt-on web analytics for Go applications. It is similar in spirit to
Umami, Plausible, or PostHog, but embedded in your app.

It does the following:

- Logs HTTP traffic
- Custom tracking events 
- Frontend browser session recording 
- Built-in dashboard

The core `plainoldanalytics` package is storage-agnostic — it knows nothing
about DuckDB or any other backend, so importing it never pulls one in. Pick a
storage package and use its `PlainOldAnalytics` constructor to get both a
working `Storage` and the `Analytics` wiring in one call, or implement the
`Storage` interface yourself and pass it to `plainoldanalytics.New`.

## Quick start

```go
package main

import (
    "log"
    "net/http"

    "jaygoel.com/plainoldanalytics/storage/memory_store"
)

func handler(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte("hello"))
}

func main() {
    analytics := memory_store.PlainOldAnalytics()
    defer analytics.Close()

    router := http.NewServeMux()
    router.HandleFunc("/", handler)

    // Built-in dashboard
    analytics.Mount(router, "/analytics")

    loggedRouter := analytics.Middleware(router)
    http.ListenAndServe(":8080", loggedRouter)
}
```

Open `/analytics` to see the analytics dashboard. 

## Frontend events and session replay

Include the snippet in your pages:

```html
<script src="/analytics/e.js"></script>
<script>
// Run this (optional) to capture browser screen recording
  plainoldanalytics.init_session_recording();           

  // Capture any events (button clicks, etc)
  plainoldanalytics.track('signup', {plan: 'pro'});   
</script>
```

## Features

### Persistent storage

Plainoldanalytics uses DuckDB for persistent storage.

``` go
import (
    "github.com/duckdb/duckdb-go/v2"
    "jaygoel.com/plainoldanalytics/storage/duckdb_store"
)

db, _ := duckdb.NewConnector("plainoldanalytics.duckdb", nil)
analytics, _ := duckdb_store.PlainOldAnalytics(db)
```

Traffic flushes to disk periodically (about once a second); call
`analytics.Close()` on shutdown to flush anything still buffered.

### Set properties for each request

You can set key/value properties on each request by calling `plainoldanalytics.Set()`. 
In the UI, you can filter on these properties. Plainoldanalytics special-cases the `user` property to
show all traffic related to that user in the UI.

``` go
func handler(w http.ResponseWriter, r *http.Request) {
    plainoldanalytics.Set(r.Context(), "user", "user@example.com")
    w.Write([]byte("hello"))
}
```

### Exclude Routes

To exclude a route from being logged, call `plainoldanalytics.Exclude()`

``` go
func handler(w http.ResponseWriter, r *http.Request) {
    plainoldanalytics.Exclude(r.Context())
    w.Write([]byte("hello"))
}
```
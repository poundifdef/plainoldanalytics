// Package plainoldanalytics provides HTTP analytics with an optional dashboard.
//
// This package is storage-agnostic: it knows nothing about any particular
// backend, so importing it never pulls one in. Pick a backend package (e.g.
// storage/duckdb_store or storage/memory_store) and use its
// PlainOldAnalytics constructor to get an *Analytics in one call, or build
// your own storage.Storage and pass it to New.
package plainoldanalytics

import (
	"context"
	"io"
	"net/http"
	"strings"

	"jaygoel.com/plainoldanalytics/adapters/capture"
	httpadapter "jaygoel.com/plainoldanalytics/adapters/http"
	"jaygoel.com/plainoldanalytics/storage"
	"jaygoel.com/plainoldanalytics/webapp"
)

// Analytics shares a store between request middleware and the dashboard.
// Create one instance per application.
type Analytics struct {
	capturer  *capture.Capturer
	dashboard http.Handler
}

// New builds analytics over any Storage implementation. Most callers should
// use a backend package's own PlainOldAnalytics constructor instead (e.g.
// storage/duckdb_store.PlainOldAnalytics or
// storage/memory_store.PlainOldAnalytics); reach for New directly only when
// bringing your own Storage.
func New(store storage.Storage) *Analytics {
	capturer := capture.New(store, storage.NewConfig())
	return &Analytics{
		capturer:  capturer,
		dashboard: webapp.New(capturer).Handler(),
	}
}

// Set attaches an attribute to the current request. Keys and values are
// stringified, and later calls for a key replace its value. Outside the
// analytics middleware it is a no-op.
func Set(ctx context.Context, key, value any) {
	capture.Set(ctx, key, value)
}

// Exclude skips recording the current request. Call it before your handler
// returns, for example to exclude health checks. Outside middleware it is a no-op.
func Exclude(ctx context.Context) {
	capture.Exclude(ctx)
}

// Mount registers the dashboard and its subpages on router. The path may have
// a trailing slash or omit it. Requests to the slashless path redirect to the
// dashboard root; both the redirect and dashboard exclude themselves from capture.
func (a *Analytics) Mount(router *http.ServeMux, path string) {
	prefix := strings.TrimSuffix(path, "/")
	router.HandleFunc(prefix+"/", a.Handler())
	if prefix != "" {
		router.HandleFunc(prefix, func(w http.ResponseWriter, r *http.Request) {
			Exclude(r.Context())
			target := prefix + "/"
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusMovedPermanently)
		})
	}
}

// Handler returns a dashboard handler that excludes itself from traffic capture.
// For manual registration, use a literal ServeMux subtree ending in /:
//
//	router.HandleFunc("/reports/", analytics.Handler())
//
// Prefer Mount to accept paths with or without a trailing slash.
// The matched route supplies the mount prefix, so links, assets, and ingestion
// endpoints work under that path. Middleware leaves routing to your router.
func (a *Analytics) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Patterns may include a method and host before the path.
		pattern := r.Pattern
		if i := strings.IndexByte(pattern, '/'); i >= 0 {
			pattern = pattern[i:]
		}
		prefix := strings.TrimSuffix(pattern, "/")
		http.StripPrefix(prefix, a.dashboard).ServeHTTP(w, r)
	}
}

// Middleware records requests unless their handlers call Exclude. The dashboard
// calls Exclude automatically, regardless of where it is mounted.
func (a *Analytics) Middleware(next http.Handler) http.Handler {
	return httpadapter.New(a.capturer).Wrap(next)
}

// Close releases the underlying storage's resources, flushing any buffered
// data first. Only meaningful for storage that outlives a single process
// run, such as DuckDB on disk; a no-op otherwise.
func (a *Analytics) Close() error {
	if c, ok := a.capturer.Storage.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

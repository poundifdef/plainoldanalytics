// Package chi instruments chi applications with plainoldanalytics.
//
// Unlike gin, whose FullPath and Params are resolved before any middleware
// in the chain runs, chi builds up the matched route on the same
// *chi.Context as routing descends the tree — its own docs recommend
// reading RoutePattern only after calling the next handler. Wrap follows
// that same read-after-next shape capture.Serve already uses for ServeMux,
// so recording the route just means copying chi's routing info onto the
// request the way ServeMux would, once it's known.
package chi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"jaygoel.com/plainoldanalytics/adapters/capture"
)

// Middleware records every request it serves via its Capturer. Construct
// with New, then register on a router with router.Use(m.Wrap).
type Middleware struct {
	cap *capture.Capturer
}

func New(c *capture.Capturer) *Middleware {
	return &Middleware{cap: c}
}

// Wrap returns next wrapped with recording. Register with router.Use(m.Wrap),
// or scope it to a route group to exclude other routes (like the dashboard)
// from capture.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.cap.Serve(w, r, "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
			// Only now, after next has run, does chi's RouteContext (the
			// same one r carries throughout) hold the fully matched route.
			// Copy it onto r the way ServeMux would, for capture.Serve's own
			// post-next read (pattern == "" above) to pick up.
			rctx := chi.RouteContext(r.Context())
			if rctx == nil {
				return
			}
			r.Pattern = stdPattern(rctx.RoutePattern())
			for i, key := range rctx.URLParams.Keys {
				r.SetPathValue(stdParamName(key), rctx.URLParams.Values[i])
			}
		}))
	})
}

// stdPattern converts a chi route pattern to ServeMux form. Named params
// ("/hello/{name}") are already identical; only chi's unnamed catch-all
// ("/f/*") differs; it becomes ServeMux's named wildcard form.
func stdPattern(pattern string) string {
	if strings.HasSuffix(pattern, "/*") {
		return strings.TrimSuffix(pattern, "*") + "{" + wildcardName + "...}"
	}
	return pattern
}

// wildcardName is the ServeMux-form name given to chi's unnamed catch-all
// ("*"), since ServeMux wildcards are always named.
const wildcardName = "wildcard"

// stdParamName maps chi's catch-all key ("*") onto the same name stdPattern
// gives it; named params pass through unchanged.
func stdParamName(key string) string {
	if key == "*" {
		return wildcardName
	}
	return key
}

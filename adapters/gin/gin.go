// Package gin instruments gin applications with plainoldanalytics. It
// translates gin's routing info (FullPath, Params) into ServeMux form so
// the capture engine and storage stay framework-agnostic.
package gin

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"jaygoel.com/plainoldanalytics/adapters/capture"
)

// Middleware records every request it serves via its Capturer. Construct
// with New, then register on a router with router.Use(m.Handle).
type Middleware struct {
	cap *capture.Capturer
}

func New(c *capture.Capturer) *Middleware {
	return &Middleware{cap: c}
}

// Handle is a gin.HandlerFunc. It translates gin's routing info (FullPath,
// Params) into ServeMux form so the recording code is framework-agnostic,
// then delegates to the capturer. The request itself is not rewritten; the
// pattern travels separately.
func (m *Middleware) Handle(c *gin.Context) {
	r := c.Request
	for _, p := range c.Params {
		r.SetPathValue(p.Key, strings.TrimPrefix(p.Value, "/"))
	}
	m.cap.Serve(c.Writer, r, stdPattern(c.FullPath()), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Request = r
		c.Next()
	}))
}

// stdPattern converts a gin route ("/b/:c", "/f/*rest") to ServeMux form
// ("/b/{c}", "/f/{rest...}"). Returns "" when no route matched, mirroring
// ServeMux's behavior on 404.
func stdPattern(fullPath string) string {
	if fullPath == "" {
		return ""
	}
	segs := strings.Split(fullPath, "/")
	for i, s := range segs {
		if name, ok := strings.CutPrefix(s, ":"); ok {
			segs[i] = "{" + name + "}"
		} else if name, ok := strings.CutPrefix(s, "*"); ok {
			segs[i] = "{" + name + "...}"
		}
	}
	return strings.Join(segs, "/")
}

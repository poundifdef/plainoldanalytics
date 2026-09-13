// Package http instruments net/http applications with plainoldanalytics.
// ServeMux populates the request's Pattern and path values natively, so the
// adapter is pure glue around the capture engine.
package http

import (
	"net/http"

	"jaygoel.com/plainoldanalytics/adapters/capture"
)

// Middleware records every request it serves via its Capturer. Construct
// with New, then wrap any handler at use time with Wrap.
type Middleware struct {
	cap *capture.Capturer
}

func New(c *capture.Capturer) *Middleware {
	return &Middleware{cap: c}
}

// Wrap returns next wrapped with recording. ServeMux populates the request's
// Pattern and path values itself, so the pattern is left for the capturer
// to read from the request after serving.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.cap.Serve(w, r, "", next)
	})
}

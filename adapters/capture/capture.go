// Package capture is the framework-agnostic capture engine: it runs a
// handler while recording status, size, duration, the visitor (via a
// server-minted tracking cookie), and any attributes the handler
// attached with Set, then hands the result to a storage.Storage.
//
// Framework middlewares (adapters/http, adapters/gin) translate their
// router's request/response types onto Capturer.Serve; writing a new
// adapter is a few lines of glue.
package capture

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"jaygoel.com/plainoldanalytics/storage"
)

// carrier is the mutable per-request container Serve seeds into the request
// context so that any handler below the middleware can attach context
// values via Set.
type carrier struct {
	mu       sync.Mutex
	vals     map[string]string
	excluded bool
}

type carrierKey struct{}

// Set attaches an analytics attribute to the current request from
// anywhere below the analytics middleware — no registration or middleware
// wrapping needed. Key and value are stringified with fmt.Sprint, so plain
// strings and typed string keys (type ctxKey string) name themselves; a
// later Set for the same key overwrites the earlier value. Outside an
// instrumented request it is a no-op. Set is safe for concurrent use; only
// values set before the handler returns are guaranteed to be recorded.
// The middleware shares a mutable bag through the context, so Set also
// works with derived contexts. Ordinary context.WithValue entries are not
// recorded.
func Set(ctx context.Context, key any, value any) {
	car, ok := ctx.Value(carrierKey{}).(*carrier)
	if !ok {
		return
	}
	car.mu.Lock()
	defer car.mu.Unlock()
	car.vals[fmt.Sprint(key)] = fmt.Sprint(value)
}

// Exclude skips recording the current request. Call it from a handler or
// middleware before returning. Outside analytics middleware it is a no-op.
func Exclude(ctx context.Context) {
	car, ok := ctx.Value(carrierKey{}).(*carrier)
	if !ok {
		return
	}
	car.mu.Lock()
	defer car.mu.Unlock()
	car.excluded = true
}

// Capturer captures request/response data and records it to a Storage.
// Framework middlewares adapt their own request/response types onto its
// Serve method. Behavior (visitor cookie) comes from the
// storage.Config, so all layers share one configuration.
type Capturer struct {
	Storage storage.Storage
	Config  storage.Config
}

// New returns a Capturer recording to s, configured by cfg (built with
// storage.NewConfig). Pass the same Capturer to a framework adapter and to
// webapp.New so middleware and dashboard share one configuration.
func New(s storage.Storage, cfg storage.Config) *Capturer {
	return &Capturer{Storage: s, Config: cfg}
}

// Serve runs next while capturing status, size, duration, visitor, and any
// context values attached with Set, then records the result. Adapters that know
// the matched route up front pass it as pattern (in ServeMux form); pass ""
// for routers that populate r.Pattern themselves (ServeMux), and it is read
// from the request after serving. The stored pattern is normalized: the
// method prefix is stripped, since the method is already on the request.
func (cp *Capturer) Serve(w http.ResponseWriter, r *http.Request, pattern string, next http.Handler) {
	visitor := cp.visitor(w, r)
	session := cp.session(w, r)

	// Seed the mutable carrier so handlers can attach context values with
	// Set without any middleware plumbing of their own.
	car := &carrier{vals: map[string]string{}}
	r = r.WithContext(context.WithValue(r.Context(), carrierKey{}, car))

	ss, ok := w.(statusSizer)
	if !ok {
		rec := &recorder{ResponseWriter: w}
		w = rec
		ss = rec
	}
	start := time.Now()
	next.ServeHTTP(w, r)
	pattern = normalizePattern(cmp.Or(pattern, r.Pattern))

	// Take ownership of the values, leaving a fresh map behind so a Set
	// from a goroutine that outlives the request mutates that discarded
	// map instead of the one handed to storage.
	var ctxVals map[string]string
	car.mu.Lock()
	if car.excluded {
		car.mu.Unlock()
		return
	}
	if len(car.vals) > 0 {
		ctxVals = car.vals
		car.vals = map[string]string{}
	}
	car.mu.Unlock()

	// The first authenticated request converts this browser's previously
	// anonymous traffic. Later identities only label their own requests.
	if user := ctxVals[cp.Config.UserKey]; user != "" {
		if s, ok := cp.Storage.(storage.IdentityStore); ok {
			if err := s.Identify(r.Context(), visitor, cp.Config.UserKey, user); err != nil {
				slog.Error("analytics identify", "error", err)
			}
		}
	}
	cp.Storage.Record(r, storage.Capture{
		Pattern:    pattern,
		Status:     ss.Status(),
		Size:       ss.Size(),
		Time:       start,
		Duration:   time.Since(start),
		PathValues: PathValues(r, pattern),
		Visitor:    visitor,
		Session:    session,
		Context:    ctxVals,
	})
}

// visitor returns the request's visitor ID, setting the tracking cookie on
// the response when the request doesn't carry a valid one yet. Visitor IDs
// are integers; the cookie carries the decimal form since cookies are text.
func (cp *Capturer) visitor(w http.ResponseWriter, r *http.Request) uint64 {
	if cp.Config.VisitorCookie == "" {
		return 0
	}
	if ck, err := r.Cookie(cp.Config.VisitorCookie); err == nil {
		if v, err := strconv.ParseUint(ck.Value, 10, 64); err == nil && v != 0 {
			return v
		}
	}
	visitor := storage.NewID()
	http.SetCookie(w, &http.Cookie{
		Name:     cp.Config.VisitorCookie,
		Value:    strconv.FormatUint(visitor, 10),
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		SameSite: http.SameSiteLaxMode,
	})
	return visitor
}

// session returns the ID for this browser session. Unlike visitor, its cookie
// has no MaxAge or Expires attribute, so browsers remove it when their
// browser session ends.
func (cp *Capturer) session(w http.ResponseWriter, r *http.Request) uint64 {
	if cp.Config.SessionCookie == "" {
		return 0
	}
	if ck, err := r.Cookie(cp.Config.SessionCookie); err == nil {
		if v, err := strconv.ParseUint(ck.Value, 10, 64); err == nil && v != 0 {
			return v
		}
	}
	session := storage.NewID()
	http.SetCookie(w, &http.Cookie{
		Name:     cp.Config.SessionCookie,
		Value:    strconv.FormatUint(session, 10),
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})
	return session
}

// normalizePattern strips the optional method prefix from a ServeMux-form
// pattern ("GET /b/{c}" -> "/b/{c}"). Patterns contain no other spaces.
func normalizePattern(pattern string) string {
	if _, path, ok := strings.Cut(pattern, " "); ok {
		return path
	}
	return pattern
}

var wildcardRE = regexp.MustCompile(`\{([^}]*)\}`)

// PathValues returns all wildcard values matched by the given ServeMux-form
// pattern, keyed by wildcard name, read from r's path values.
func PathValues(r *http.Request, pattern string) map[string]string {
	vals := map[string]string{}
	for _, m := range wildcardRE.FindAllStringSubmatch(pattern, -1) {
		name := strings.TrimSuffix(m[1], "...")
		if name == "" || name == "$" {
			continue
		}
		vals[name] = r.PathValue(name)
	}
	return vals
}

// statusSizer is satisfied by response writers that already track their own
// status and size (e.g. gin's ResponseWriter). Serve reads from those
// directly instead of wrapping them in a recorder.
type statusSizer interface {
	Status() int
	Size() int
}

type recorder struct {
	http.ResponseWriter
	status int
	size   int
}

func (rec *recorder) WriteHeader(code int) {
	rec.status = code
	rec.ResponseWriter.WriteHeader(code)
}

func (rec *recorder) Write(b []byte) (int, error) {
	n, err := rec.ResponseWriter.Write(b)
	rec.size += n
	return n, err
}

// Unwrap lets http.NewResponseController reach the underlying writer's
// optional interfaces (Flusher, Hijacker, ...) through the recorder.
func (rec *recorder) Unwrap() http.ResponseWriter { return rec.ResponseWriter }

// ReadFrom keeps the sendfile path alive: io.Copy sees the underlying
// writer's io.ReaderFrom instead of falling back to a buffered loop through
// the recorder. Status is set first because the underlying ReadFrom triggers
// the implicit WriteHeader(200) on the real writer, bypassing our interceptor.
func (rec *recorder) ReadFrom(src io.Reader) (int64, error) {
	if rec.status == 0 {
		rec.status = http.StatusOK
	}
	n, err := io.Copy(rec.ResponseWriter, src)
	rec.size += int(n)
	return n, err
}

// Status treats an unset status as the implicit 200 the server sends when a
// handler never calls WriteHeader.
func (rec *recorder) Status() int { return cmp.Or(rec.status, http.StatusOK) }
func (rec *recorder) Size() int   { return rec.size }

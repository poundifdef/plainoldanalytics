package storage

import "fmt"

// Config carries the analytics configuration shared by every layer: the
// capture middleware reads the visitor cookie name, the web app reads the
// user key. Build one with NewConfig and pass the same value everywhere so
// the layers agree.
type Config struct {
	// UserKey is the context-value name the UI treats as "the user": shown
	// as a User column and filterable by clicking. It is just a name —
	// storage does not special-case it. Handlers attach it with
	// capture.Set, like any other context value.
	UserKey string

	// VisitorCookie is the name of the long-lived tracking cookie that
	// identifies a browser across browser sessions. Empty disables visitor
	// tracking.
	VisitorCookie string

	// SessionCookie is the name of the session-only cookie that identifies
	// one browser session. It deliberately has no expiry, so the browser
	// discards it when the browser session ends. Empty disables sessions.
	SessionCookie string
}

// Option configures a Config.
type Option func(*Config)

// NewConfig returns a Config with defaults (UserKey "user", VisitorCookie
// "poa_visitor") applied, then the given options.
func NewConfig(opts ...Option) Config {
	c := Config{
		UserKey:       "user",
		VisitorCookie: "poa_visitor",
		SessionCookie: "poa_session",
	}
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// WithUserKey sets which context-value name the UI treats as "the user".
// The name is the key's string form (fmt.Sprint), matching how capture.Set
// stringifies its key, so plain strings and typed string keys (type ctxKey
// string) name themselves.
func WithUserKey(key any) Option {
	return func(c *Config) { c.UserKey = fmt.Sprint(key) }
}

// WithVisitorCookie sets the visitor tracking cookie's name; pass "" to
// disable visitor tracking entirely.
func WithVisitorCookie(name string) Option {
	return func(c *Config) { c.VisitorCookie = name }
}

// WithSessionCookie sets the browser-session cookie's name; pass "" to
// disable browser-session tracking.
func WithSessionCookie(name string) Option {
	return func(c *Config) { c.SessionCookie = name }
}

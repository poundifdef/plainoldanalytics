package storage

import (
	"net/http/httptest"
	"testing"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		headers    map[string]string
		want       string
	}{
		{"direct", "1.2.3.4:5678", nil, "1.2.3.4"},
		{"direct ipv6", "[2001:db8::1]:5678", nil, "2001:db8::1"},
		{"xff single", "10.0.0.1:80", map[string]string{"X-Forwarded-For": "203.0.113.7"}, "203.0.113.7"},
		{"xff chain", "10.0.0.1:80", map[string]string{"X-Forwarded-For": "203.0.113.7, 10.0.0.2, 10.0.0.1"}, "203.0.113.7"},
		{"xff garbage falls through", "10.0.0.1:80", map[string]string{"X-Forwarded-For": "not-an-ip"}, "10.0.0.1"},
		{"x-real-ip", "10.0.0.1:80", map[string]string{"X-Real-Ip": "203.0.113.9"}, "203.0.113.9"},
		{"xff wins over x-real-ip", "10.0.0.1:80", map[string]string{"X-Forwarded-For": "203.0.113.7", "X-Real-Ip": "203.0.113.9"}, "203.0.113.7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			if got := ClientIP(req); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewID(t *testing.T) {
	a, b := NewID(), NewID()
	if a == 0 || b == 0 || a == b {
		t.Errorf("ids not unique: %d %d", a, b)
	}
	if b < a {
		t.Errorf("ids not time-ordered: %d then %d", a, b)
	}
}

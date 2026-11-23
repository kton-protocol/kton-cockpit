package watch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoopbackOnly_RefusesForeignHostsAndOrigins(t *testing.T) {
	hit := 0
	h := LoopbackOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hit++ }))
	cases := []struct {
		host, origin string
		ok           bool
	}{
		{"127.0.0.1:8378", "", true}, // the extension (Node), the cockpit, curl
		{"localhost:8378", "", true},
		{"[::1]:8378", "", true},
		{"127.0.0.1:8378", "http://127.0.0.1:8378", true}, // the viewer ktonwatch serves
		{"127.0.0.1:8378", "http://localhost:8378", true},
		{"127.0.0.1:8378", "vscode-webview://abc123", true}, // the extension's panels
		{"evil.example:8378", "", false},                    // DNS rebinding
		{"evil.example", "http://evil.example", false},
		{"127.0.0.1:8378", "https://evil.example", false},  // a cross-origin page
		{"127.0.0.1:8378", "http://127.0.0.1:9999", false}, // another local server's page
		{"127.0.0.1:8378", "null", false},                  // a sandboxed or file page
	}
	for _, c := range cases {
		hit = 0
		r := httptest.NewRequest("POST", "/api/declared", strings.NewReader("{}"))
		r.Host = c.host
		r.Header.Set("Content-Type", "text/plain")
		if c.origin != "" {
			r.Header.Set("Origin", c.origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if got := w.Code != http.StatusForbidden && hit == 1; got != c.ok {
			t.Errorf("Host %q Origin %q: passed=%v, want %v (code %d)", c.host, c.origin, got, c.ok, w.Code)
		}
	}
}

package httpguard

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// A loopback door answers its own clients and pages, and refuses a rebinding page's Host and another
// origin's page, before the handler runs (kton-review on #236).
func TestLoopbackOnly(t *testing.T) {
	ran := false
	h := LoopbackOnly("test door", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { ran = true }))
	for _, c := range []struct {
		host, origin string
		ok           bool
	}{
		{"127.0.0.1:8377", "", true},
		{"localhost:8377", "", true},
		{"[::1]:8377", "", true},
		{"127.0.0.1:8377", "http://127.0.0.1:8377", true},
		{"127.0.0.1:8377", "http://localhost:8377", true},
		{"127.0.0.1:8377", "vscode-webview://abc", true},
		{"evil.example:8377", "", false},                       // DNS rebinding: the page's name as Host
		{"127.0.0.1:8377", "https://evil.example", false},      // a cross-site page
		{"127.0.0.1:8377", "http://127.0.0.1:9999", false},     // another loopback door's page
		{"127.0.0.1:8377", "null", false},                      // a sandboxed page
		{"192.168.1.5:8377", "http://192.168.1.5:8377", false}, // not the loopback
	} {
		ran = false
		req := httptest.NewRequest(http.MethodPost, "http://"+c.host+"/", nil)
		req.Host = c.host
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if c.ok != ran || (!c.ok && rec.Code != http.StatusForbidden) {
			t.Errorf("Host %s Origin %q: want answered=%v, got ran=%v code=%d", c.host, c.origin, c.ok, ran, rec.Code)
		}
	}
}

func TestListensOnLoopback(t *testing.T) {
	for addr, want := range map[string]bool{"127.0.0.1:8377": true, "localhost:8377": true, "[::1]:1": true, ":8377": false, "0.0.0.0:8377": false, "192.168.1.5:80": false} {
		if got := ListensOnLoopback(addr); got != want {
			t.Errorf("%s: %v, want %v", addr, got, want)
		}
	}
}

func TestAddressOnlyAndWildcard(t *testing.T) {
	ran := false
	h := AddressOnly("test door", "192.168.1.5:8377", http.HandlerFunc(func(http.ResponseWriter, *http.Request) { ran = true }))
	for _, c := range []struct {
		host, origin string
		ok           bool
	}{
		{"192.168.1.5:8377", "", true},
		{"192.168.1.5:8377", "http://192.168.1.5:8377", true},
		{"evil.example:8377", "", false},
		{"192.168.1.5:8377", "https://evil.example", false},
	} {
		ran = false
		req := httptest.NewRequest(http.MethodGet, "http://"+c.host+"/", nil)
		req.Host = c.host
		if c.origin != "" {
			req.Header.Set("Origin", c.origin)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if c.ok != ran {
			t.Errorf("Host %s Origin %q: want answered=%v, got %v (%d)", c.host, c.origin, c.ok, ran, rec.Code)
		}
	}
	for addr, want := range map[string]bool{":8377": true, "0.0.0.0:8377": true, "[::]:8377": true, "192.168.1.5:8377": false, "127.0.0.1:8377": false} {
		if got := Wildcard(addr); got != want {
			t.Errorf("Wildcard(%s) = %v, want %v", addr, got, want)
		}
	}
}

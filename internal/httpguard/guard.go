// Package httpguard keeps a web page out of a door the cockpit opens on this machine's loopback.
//
// A server that listens on 127.0.0.1 is still reached by every browser on the machine: by a
// cross-origin request (a simple text/plain POST needs no preflight), or by DNS rebinding (a name of
// the page's own that resolves to 127.0.0.1, so the request carries that name as its Host). So a
// loopback door answers only a Host that is the loopback, and, when the request carries an Origin, only
// its own pages or a VS Code webview; the cockpit's own clients (Go, Node, curl) send none
// (kton-review on #236; ktonwatch's guard, recorder/watch/guard.go, is the same rule).
package httpguard

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// LoopbackOnly refuses, with 403 before next runs, a request whose Host is not the loopback or whose
// Origin is another page's. what names the door in the refusal.
func LoopbackOnly(what string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !LoopbackHost(r.Host) {
			http.Error(w, what+" answers on the loopback only (Host "+r.Host+")", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !trustedOrigin(o, r.Host) {
			http.Error(w, what+" does not answer pages of other origins ("+o+")", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// LoopbackHost says whether hostport names the loopback: an address of it, or localhost, with any port.
func LoopbackHost(hostport string) bool {
	h := hostport
	if x, _, err := net.SplitHostPort(hostport); err == nil {
		h = x
	}
	h = strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")
	if strings.EqualFold(h, "localhost") {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// AddressOnly refuses, with 403 before next runs, a request whose Host is not addr (a server bound to one
// address of this machine: a rebinding name is refused) or whose Origin is another page's.
func AddressOnly(what, addr string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Host, addr) {
			http.Error(w, what+" answers at "+addr+" only (Host "+r.Host+")", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" {
			if u, err := url.Parse(o); err != nil || u.Scheme != "http" || !strings.EqualFold(u.Host, addr) {
				http.Error(w, what+" does not answer pages of other origins ("+o+")", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Wildcard says whether a listen address binds every interface (":8377", "0.0.0.0:8377", "[::]:8377").
func Wildcard(addr string) bool {
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(h)
	return h == "" || (ip != nil && ip.IsUnspecified())
}

// ListensOnLoopback says whether a listen address binds the loopback only (not ":8377", which binds
// every interface).
func ListensOnLoopback(addr string) bool {
	h, _, err := net.SplitHostPort(addr)
	return err == nil && h != "" && LoopbackHost(addr)
}

func trustedOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "vscode-webview": // an extension's panel: code the person installed and runs locally
		return true
	case "http":
		return LoopbackHost(u.Host) && (u.Host == host || samePort(u.Host, host))
	}
	return false
}

// samePort: 127.0.0.1:8377 and localhost:8377 are the same door.
func samePort(a, b string) bool {
	_, pa, ea := net.SplitHostPort(a)
	_, pb, eb := net.SplitHostPort(b)
	return ea == nil && eb == nil && pa == pb && LoopbackHost(a) && LoopbackHost(b)
}

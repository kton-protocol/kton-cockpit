package watch

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// LoopbackOnly guards ktonwatch's HTTP door (kton-review C1 on #236). ktonwatch listens on the
// loopback, but a web page in any browser on the machine can still reach it: by a cross-origin
// request (a simple text/plain POST needs no preflight), or by DNS rebinding (a name of its own
// that resolves to 127.0.0.1, so the request carries its Host). So:
//   - the Host must be the loopback, by address or as localhost (with any port);
//   - a request that carries an Origin must come from ktonwatch itself (the viewer it serves) or
//     from a VS Code webview; the extension and the cockpit call from Node and Go and send none.
//
// Everything else is refused with 403 before any handler runs.
func LoopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			http.Error(w, "ktonwatch answers on the loopback only (Host "+r.Host+")", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && !trustedOrigin(o, r.Host) {
			http.Error(w, "ktonwatch does not answer pages of other origins ("+o+")", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func loopbackHost(hostport string) bool {
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

func trustedOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	switch u.Scheme {
	case "vscode-webview": // the extension's own panels (the story, the In kton tab)
		return true
	case "http":
		return loopbackHost(u.Host) && (u.Host == host || sameLoopbackPort(u.Host, host))
	}
	return false
}

// sameLoopbackPort: 127.0.0.1:8378 and localhost:8378 are the same ktonwatch.
func sameLoopbackPort(a, b string) bool {
	_, pa, ea := net.SplitHostPort(a)
	_, pb, eb := net.SplitHostPort(b)
	return ea == nil && eb == nil && pa == pb && loopbackHost(a) && loopbackHost(b)
}

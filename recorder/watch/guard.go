package watch

import (
	"net/http"

	"github.com/kton-protocol/kton-cockpit/internal/httpguard"
)

// LoopbackOnly guards ktonwatch's HTTP door (kton-review C1 on #236) with the cockpit's shared rule
// (internal/httpguard, kton-cockpit #239): the Host must be the loopback, and a request that carries an
// Origin must come from ktonwatch's own viewer or a VS Code webview; everything else gets 403 before any
// handler runs. The extension and the cockpit call from Node and Go and send no Origin.
func LoopbackOnly(next http.Handler) http.Handler { return httpguard.LoopbackOnly("ktonwatch", next) }

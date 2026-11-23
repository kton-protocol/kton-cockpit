package cli

import (
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kton-protocol/kton-cockpit/internal/testrepo"
)

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// get sends a request to addr with the Host and Origin given, retrying while the server starts.
func get(t *testing.T, method, addr, path, host, origin string) int {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		req, _ := http.NewRequest(method, "http://"+addr+path, strings.NewReader(`{}`))
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		res, err := http.DefaultClient.Do(req)
		if err == nil {
			res.Body.Close()
			return res.StatusCode
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s did not answer: %v", addr, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// cockpit show and cockpit shim serve on TCP answer their own clients and refuse a rebinding page's
// Host and another origin's page (kton-review on #236).
func TestLoopbackDoors_RefuseOtherPages(t *testing.T) {
	r := testrepo.New(t)
	for _, door := range []struct {
		name   string
		args   func(addr string) []string
		method string
		path   string
	}{
		{"show", func(a string) []string { return []string{"show", a} }, http.MethodGet, "/data/keys.json"},
		{"shim serve", func(a string) []string { return []string{"shim", "serve", "--listen", a} }, http.MethodPost, "/"},
	} {
		addr := freePort(t)
		cmd := r.CockpitCommand(t, door.args(addr)...)
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
		if code := get(t, door.method, addr, door.path, addr, ""); code == http.StatusForbidden {
			t.Errorf("%s: its own client is refused (%d)", door.name, code)
		}
		_, port, _ := net.SplitHostPort(addr)
		for _, c := range []struct{ host, origin string }{
			{"evil.example:" + port, ""},
			{addr, "https://evil.example"},
		} {
			if code := get(t, door.method, addr, door.path, c.host, c.origin); code != http.StatusForbidden {
				t.Errorf("%s: Host %s Origin %q: want 403, got %d", door.name, c.host, c.origin, code)
			}
		}
	}
}

package cockpit

import (
	"encoding/json"
	"errors"
	"net/http"
)

// maxCallBytes bounds one call's body: the inputs of a NONMEM run are kilobytes to megabytes.
const maxCallBytes = 1 << 30

// CallHandler serves Call over HTTP for the tool shims of a calling environment (ADR-014): POST a
// ToolCall as JSON, get a ToolResult back. It offers Call and nothing else, so whoever mounts it
// into a container hands that container no other verb (ADR-014, section 1). The listener, and any token on it,
// belong to whoever mounts the handler: `cockpit shim serve` here, ktonwatch's /v1/call there.
func (c *Cockpit) CallHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.Error(w, "POST a tool call", http.StatusMethodNotAllowed)
			return
		}
		var in ToolCall
		if err := json.NewDecoder(http.MaxBytesReader(w, req.Body, maxCallBytes)).Decode(&in); err != nil {
			http.Error(w, "not a tool call: "+err.Error(), http.StatusBadRequest)
			return
		}
		res, err := c.Call(req.Context(), in)
		w.Header().Set("Content-Type", "application/json")
		var ref *Refusal
		switch {
		case errors.As(err, &ref):
			w.WriteHeader(http.StatusUnprocessableEntity)
			json.NewEncoder(w).Encode(ref)
		case err != nil:
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(&Refusal{Code: "error", Reason: err.Error()})
		default:
			json.NewEncoder(w).Encode(res)
		}
	})
}

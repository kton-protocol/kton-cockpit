//go:build !unreleased

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
)

// install and workflow read packages in the scope format (ktonpkg/scope), which no published kernel
// carries yet. This build is against published kernels only, so both say so instead of running.
var errUnreleased = errors.New("not in this build: install and workflow need the scope format, which is not published yet (build with -tags unreleased)")

func runInstall(context.Context, []string) error  { return errUnreleased }
func runWorkflow(context.Context, []string) error { return errUnreleased }

func emitJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

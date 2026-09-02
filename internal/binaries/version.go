package binaries

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
)

// RequiredKernelMajor/Minor is the oldest kton kernel this cockpit can drive. It is a real
// requirement, not a preference: three things the cockpit does have no pre-0.2 form.
//
//   - `nekton about --json` / `nekton by --json` (#39/#40) and `--json` on plankton's read surface
//     (#57). Without them the claim axis — what a claim actually says — is unreachable, and a
//     record's id has to be guessed at as the first hash on a line of prose.
//   - `plankton reproductions --trust-keys`. Without it the ↻N count is self-declared and
//     forgeable, and serving a forgeable number is worse than serving none.
//   - `nekton annotate --print-id` (#56). Without it the claim id has to be scraped back out of
//     four lines of prose printed to stdout.
//
// A 0.1 binary rejects all three on its usage line, which reads as a confusing syntax error rather
// than "your kernel is too old" — hence this check, rather than letting each call fail on its own.
const (
	RequiredKernelMajor = 0
	RequiredKernelMinor = 2
)

// versionRe matches what `plankton version` / `nekton version` print, e.g.
// `plankton 0.2 (reference)`.
var versionRe = regexp.MustCompile(`\b(\d+)\.(\d+)\b`)

// KernelVersions reports the version each binary states about itself.
func (r *Runner) KernelVersions(ctx context.Context) (plankton, nekton string, err error) {
	if plankton, err = r.plankton(ctx, "version"); err != nil {
		return "", "", fmt.Errorf("could not run plankton: %w", err)
	}
	if nekton, err = r.nekton(ctx, "version"); err != nil {
		return "", "", fmt.Errorf("could not run nekton: %w", err)
	}
	return plankton, nekton, nil
}

// CheckKernel fails if either binary is older than this cockpit requires. It reads each binary's
// own reported version rather than a recorded or configured one: what matters is which build will
// actually be executed, not which one someone believes is installed.
func (r *Runner) CheckKernel(ctx context.Context) error {
	plankton, nekton, err := r.KernelVersions(ctx)
	if err != nil {
		return err
	}
	for name, reported := range map[string]string{"plankton": plankton, "nekton": nekton} {
		ok, major, minor := parseKernelVersion(reported)
		if !ok {
			return fmt.Errorf("could not read a version out of what %s reports (%q)", name, reported)
		}
		if major < RequiredKernelMajor || (major == RequiredKernelMajor && minor < RequiredKernelMinor) {
			return fmt.Errorf(
				"%s is %d.%d, but this cockpit requires %d.%d or newer: `nekton about/by --json`, "+
					"`nekton annotate --print-id` and `plankton reproductions --trust-keys` do not exist "+
					"before %d.%d, and the cockpit has no fallback for any of them — the pre-%d.%d "+
					"alternatives are unparseable prose and a forgeable count. Rebuild the binaries from a "+
					"kton checkout (see CLAUDE.md, \"The kernel binaries\")",
				name, major, minor, RequiredKernelMajor, RequiredKernelMinor,
				RequiredKernelMajor, RequiredKernelMinor, RequiredKernelMajor, RequiredKernelMinor)
		}
	}
	return nil
}

func parseKernelVersion(reported string) (ok bool, major, minor int) {
	m := versionRe.FindStringSubmatch(reported)
	if m == nil {
		return false, 0, 0
	}
	major, _ = strconv.Atoi(m[1])
	minor, _ = strconv.Atoi(m[2])
	return true, major, minor
}

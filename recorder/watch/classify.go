package watch

import (
	"path/filepath"
	"strings"
)

// Class says where a path belongs.
type Class string

const (
	ClassProject Class = "project" // under the project: inputs and outputs that matter
	ClassEnv     Class = "env"     // the environment: R_HOME, .libPaths(), NONMEM, compilers …
	ClassSystem  Class = "system"  // /proc, /etc, … never an input
	ClassTemp    Class = "temp"    // temporary files of R, compilers, tools
	ClassOutside Class = "outside" // anything else: an input from outside the project
	ClassSelf    Class = "self"    // the log itself
)

// Classifier sorts paths. Longer matches are not needed: the first matching rule in the order
// self, project, temp, env, system decides, so a project inside /tmp stays a project.
type Classifier struct {
	Log     string
	Project string
	Env     []string
	System  []string
	Temp    []string // prefixes; "/tmp/Rtmp" matches /tmp/RtmpAbC/…
}

// DefaultSystem are prefixes that are never data.
var DefaultSystem = []string{"/proc/", "/sys/", "/dev/", "/etc/", "/run/", "/usr/share/", "/var/lib/", "/var/cache/", "/usr/lib/os-release"}

// DefaultTemp covers what the spikes saw: R's tempdir, gcc's assembler files, Positron's
// kernel registration files, callr/processx scratch files.
var DefaultTemp = []string{"/tmp/Rtmp", "/tmp/cc", "/tmp/registration_", "/tmp/kernel-", "/tmp/kallichore-", "/tmp/.lark_cache", "/tmp/quarto-session", "/var/tmp/Rtmp"}

// DefaultEnv are environment prefixes independent of any language runtime.
func DefaultEnv(home string) []string {
	// all of /usr/lib (R, Python, shared libraries), installed software under /usr/local and /opt:
	// a shell session without ktonrec has no ENV line that would name them
	env := []string{"/usr/lib/", "/usr/local/lib/", "/opt/", "/lib/", "/lib64/", "/usr/bin/", "/usr/local/bin/", "/bin/", "/usr/libexec/", "/usr/include/"}
	if home != "" {
		env = append(env,
			filepath.Join(home, ".cache")+"/", // caches of R, Quarto, Ark (oak) …
			filepath.Join(home, ".positron-server")+"/",
			filepath.Join(home, ".local", "share", "uv")+"/",
			filepath.Join(home, ".local", "opt")+"/",                    // software installed by hand (NONMEM, PsN)
			filepath.Join(home, ".local", "bin")+"/",                    // wrappers (nmfe75), uv, bbi
			filepath.Join(home, ".local", "lib")+"/",                    // the recorder's shim, Python user site
			filepath.Join(home, "R", "x86_64-pc-linux-gnu-library")+"/", // R's default user library
			filepath.Join(home, "R", "aarch64-unknown-linux-gnu-library")+"/",
			filepath.Join(home, ".local", "share", "kton")+"/", // the recorder's own R files
			filepath.Join(home, ".gitconfig"),
		)
	}
	return env
}

func under(p, prefix string) bool {
	if prefix == "" {
		return false
	}
	if strings.HasSuffix(prefix, "/") {
		return strings.HasPrefix(p, prefix)
	}
	return p == prefix || strings.HasPrefix(p, prefix+"/")
}

// Class sorts one absolute path.
func (c *Classifier) Class(p string) Class {
	switch {
	case c.Log != "" && p == c.Log:
		return ClassSelf
	case c.Project != "" && (p == c.Project || strings.HasPrefix(p, c.Project+"/")):
		return ClassProject
	}
	for _, t := range c.Temp {
		if strings.HasPrefix(p, t) {
			return ClassTemp
		}
	}
	for _, e := range c.Env {
		if under(p, e) {
			return ClassEnv
		}
	}
	for _, s := range c.System {
		if strings.HasPrefix(p, s) {
			return ClassSystem
		}
	}
	return ClassOutside
}

// AddEnv adds environment prefixes, e.g. from an ENV line a session wrote.
func (c *Classifier) AddEnv(prefixes ...string) {
	for _, p := range prefixes {
		if p = strings.TrimSpace(p); p != "" {
			c.Env = append(c.Env, p)
		}
	}
}

// Rel shows a path relative to the project when it is inside it.
func (c *Classifier) Rel(p string) string {
	if c.Project != "" {
		if r, err := filepath.Rel(c.Project, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	return p
}

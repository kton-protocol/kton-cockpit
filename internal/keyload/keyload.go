// Package keyload is the one place the cockpit reads a private signing key (ADR-021). Every signer
// goes through Load, so a key file in either form, the plain hex seed or the OpenSSH form encrypted
// with a passphrase, works wherever the other does. The format and the decryption are the kernel's
// (kton.dev/plankton/keys); this package decides only where a passphrase comes from, and refuses
// with a LockedError when there is none.
//
// The public half of a key is read with PublicKey and never needs a passphrase: a listing, a trust
// check and doctor name a key without unlocking it.
package keyload

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"sync"

	"golang.org/x/term"
	"kton.dev/plankton/core"
	"kton.dev/plankton/keys"
)

// LockedError is an encrypted key that could not be opened: there is no passphrase source, or the
// passphrase given is wrong. It names the key file, its keyid when the public half could be read, and
// the ways to unlock it. It never holds a passphrase. The cockpit reports it as identity.locked.
type LockedError struct {
	Path  string
	KeyID string
	// Wrong is set when a passphrase was given and did not decrypt the key.
	Wrong bool
}

func (e *LockedError) Error() string {
	who := e.Path
	if e.KeyID != "" {
		who += " (keyid " + e.KeyID + ")"
	}
	if e.Wrong {
		return who + ": wrong passphrase; nothing was signed"
	}
	return who + ": the key is encrypted and no passphrase was given; unlock it by running on a terminal " +
		"(the passphrase is asked there) or, for CI, by setting " + keys.EnvPassphrase + " in the environment of the command"
}

// mode is where a passphrase may come from in this process. The default is the safest: a process
// that never says what it is takes no passphrase at all (ADR-021 section 2.4), so a program that
// forgets to opt in refuses an encrypted key instead of signing with a value in its environment.
type mode int

const (
	// modeServer takes no passphrase: no terminal, no KTON_KEY_PASSPHRASE, nothing kept. It is the
	// default, and what mcp, serve and the shim are. A value in a server's environment must not make it
	// a signing oracle for whatever reaches its connection.
	modeServer mode = iota
	// modeEnv is a one-shot run without a terminal (a Go program, a test, CI): KTON_KEY_PASSPHRASE.
	modeEnv
	// modeTerminal is a command run by a person: the terminal first, then KTON_KEY_PASSPHRASE, and a key
	// unlocked once is not asked again in the process (one prompt per command, section 4).
	modeTerminal
)

// defaultMode is what a process is until it opts in, and what Forget returns it to.
var defaultMode = modeServer

var (
	mu       sync.Mutex
	cur      = defaultMode
	unlocked = map[string]ed25519.PrivateKey{}
)

// SetOneShot opts this process in as a one-shot run that ends when its work is done: only then does
// an encrypted key take a passphrase, from the terminal when terminal is set and there is one, else
// from KTON_KEY_PASSPHRASE. The command line sets it for the commands it knows are one-shot; a Go
// program embedding the cockpit sets it itself or gets identity.locked.
func SetOneShot(terminal bool) {
	mu.Lock()
	defer mu.Unlock()
	cur = modeEnv
	if terminal {
		cur = modeTerminal
	}
}

// SetServer marks this process a server, which is also the default: it drops the passphrase main took
// from the environment and from then on takes none. A lane that signs through MCP keeps a plain agent
// key; a person's key signs on the command line.
func SetServer() {
	mu.Lock()
	cur = modeServer
	for k, v := range unlocked {
		clear(v)
		delete(unlocked, k)
	}
	mu.Unlock()
	keys.ForgetEnvPassphrase()
	os.Unsetenv(keys.EnvPassphrase)
}

// Load reads the private key at path in either form. An encrypted key takes its passphrase from the
// terminal (a command run by a person), else from the value TakeEnvPassphrase holds
// (KTON_KEY_PASSPHRASE, which main took before anything started a child), and only in a process that
// opted in with SetOneShot; a server, and a process that never said, take none. With no passphrase
// it is refused with a *LockedError, and so it is with a wrong one. Other refusals of the kernel's
// loader (a key that is not Ed25519, a cipher it does not read, more bcrypt rounds than it accepts)
// pass through as they are.
func Load(path string) (ed25519.PrivateKey, error) {
	mu.Lock()
	m := cur
	if k, ok := unlocked[path]; ok && m == modeTerminal {
		mu.Unlock()
		return append(ed25519.PrivateKey(nil), k...), nil
	}
	mu.Unlock()

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	defer clear(b)
	k, err := keys.Parse(b, path, passphrase(m, b))
	if err != nil {
		if errors.Is(err, errNoSource) || errors.Is(err, keys.ErrNoPassphrase) {
			return nil, locked(path, b, false)
		}
		if errors.Is(err, keys.ErrWrongPassphrase) {
			return nil, locked(path, b, true)
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m == modeTerminal && keys.IsEncrypted(b) {
		mu.Lock()
		unlocked[path] = append(ed25519.PrivateKey(nil), k...)
		mu.Unlock()
	}
	return k, nil
}

var errNoSource = errors.New("no passphrase source")

func locked(path string, b []byte, wrong bool) error {
	e := &LockedError{Path: path, Wrong: wrong}
	if pub, err := keys.PublicKey(b); err == nil {
		e.KeyID = core.KeyIDHex(pub)
	}
	return e
}

// passphrase is the Passphrase handed to the kernel's loader, by the mode: none in a server, the held
// environment value in modeEnv, the terminal (when there is one) and then the environment value in
// modeTerminal. It is read per call, so a value set after start (a test) is still taken, and the
// variable is unset when it is.
func passphrase(m mode, keyBytes []byte) keys.Passphrase {
	return func(keyPath string) ([]byte, error) {
		if m == modeServer {
			return nil, errNoSource
		}
		if m == modeTerminal {
			if p, err := fromTerminal(keyPath, keyBytes); err == nil {
				return p, nil
			}
		}
		if p, ok := keys.TakeEnvPassphrase(); ok {
			return p, nil
		}
		return nil, errNoSource
	}
}

// fromTerminal asks on the controlling terminal (never stdin), naming the key and its keyid.
func fromTerminal(keyPath string, keyBytes []byte) ([]byte, error) {
	t, closeFn, err := keys.OpenTerminal()
	if err != nil {
		return nil, err
	}
	defer closeFn()
	if !term.IsTerminal(int(t.In.Fd())) {
		return nil, errors.New("not a terminal")
	}
	who := keyPath
	if pub, err := keys.PublicKey(keyBytes); err == nil {
		who += " (keyid " + core.KeyIDHex(pub) + ")"
	}
	fmt.Fprint(t.Out, "passphrase for "+who+": ")
	p, err := term.ReadPassword(int(t.In.Fd()))
	fmt.Fprintln(t.Out)
	return p, err
}

// PublicKey is the public half of the key file at path, read without a passphrase.
func PublicKey(path string) (ed25519.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	defer clear(b)
	pub, err := keys.PublicKey(b)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return pub, nil
}

// IsEncrypted says whether the key file at path needs a passphrase, from the file alone.
func IsEncrypted(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	defer clear(b)
	return keys.IsEncrypted(b)
}

// Forget drops what this process holds: the unlocked keys and the held environment passphrase. The
// command line calls it when a command is done.
func Forget() {
	SetServer()
	mu.Lock()
	cur = defaultMode
	mu.Unlock()
}

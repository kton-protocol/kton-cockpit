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

var (
	mu sync.Mutex
	// interactive is set by the command line: it may ask on the controlling terminal, and a key it
	// unlocked stays unlocked for the rest of the process (ADR-021 section 4, one prompt per command). Every
	// other caller (the MCP server, serve, a Go program, a test) has no one to ask and keeps nothing.
	interactive bool
	unlocked    = map[string]ed25519.PrivateKey{}
	// server is set by the long-running servers (mcp, serve, shim): they never read a passphrase from
	// anywhere, so an encrypted key is always a *LockedError there (ADR-021 sections 2.4 and 4).
	server bool
)

// SetServer marks this process a server: it drops the passphrase main took from the environment and
// from then on takes none, whether from the terminal or from KTON_KEY_PASSPHRASE. A value in the
// environment of a server must not make it a signing oracle for whatever reaches its connection;
// a lane that signs through MCP keeps a plain agent key. A person's key signs on the command line.
func SetServer(on bool) {
	mu.Lock()
	server = on
	mu.Unlock()
	if on {
		SetInteractive(false)
		keys.ForgetEnvPassphrase()
		os.Unsetenv(keys.EnvPassphrase)
	}
}

// SetInteractive says whether this process may ask a person on its terminal for a passphrase and
// remember what it unlocked until it ends. Only the command line's one-shot commands set it.
func SetInteractive(on bool) {
	mu.Lock()
	defer mu.Unlock()
	interactive = on
	if !on {
		for k, v := range unlocked {
			clear(v)
			delete(unlocked, k)
		}
	}
}

// Load reads the private key at path in either form. An encrypted key takes its passphrase from the
// terminal (command line only), else from the value TakeEnvPassphrase holds (KTON_KEY_PASSPHRASE,
// which main took before anything started a child; not in a server, see SetServer); with neither it is refused with a *LockedError,
// and so it is with a wrong passphrase. Other refusals of the kernel's loader (a key that is not
// Ed25519, a cipher it does not read, more bcrypt rounds than it accepts) pass through as they are.
func Load(path string) (ed25519.PrivateKey, error) {
	mu.Lock()
	if k, ok := unlocked[path]; ok && interactive {
		mu.Unlock()
		return append(ed25519.PrivateKey(nil), k...), nil
	}
	ask := interactive
	mu.Unlock()

	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	defer clear(b)
	k, err := keys.Parse(b, path, passphrase(ask, b))
	if err != nil {
		if errors.Is(err, errNoSource) || errors.Is(err, keys.ErrNoPassphrase) {
			return nil, locked(path, b, false)
		}
		if errors.Is(err, keys.ErrWrongPassphrase) {
			return nil, locked(path, b, true)
		}
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if ask && keys.IsEncrypted(b) {
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

// passphrase is the Passphrase handed to the kernel's loader: the terminal when this process may ask
// and has one, else the held environment value, else errNoSource. It is read per call, so a value
// set after start (a test) is still taken, and the variable is unset when it is.
func passphrase(ask bool, keyBytes []byte) keys.Passphrase {
	return func(keyPath string) ([]byte, error) {
		mu.Lock()
		never := server
		mu.Unlock()
		if never {
			return nil, errNoSource
		}
		if ask {
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
	SetInteractive(false)
	keys.ForgetEnvPassphrase()
}

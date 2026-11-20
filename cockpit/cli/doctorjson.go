package cli

// doctorjson.go is `doctor --json`: the same readiness as the text, for a program to read rather
// than parse. The fields are the configured core a frontend shows beside the badge; everything the
// text explains at length (scopes, material, anchor detail) stays in the text.

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/verify"
	"kton.dev/plankton/core"
)

// errNotReady is doctor having run and found a step marked ! left. It is not a failure to run, so
// it leaves by its own exit code: 1 is "doctor could not run", 2 is the cockpit refusing.
var errNotReady = errors.New("not ready")

const exitNotReady = 3

const (
	stateOK      = "ok"
	stateMissing = "missing"
	stateMade    = "made on first write"
	stateExposed = "readable by others"
)

type doctorReport struct {
	Ready  bool         `json:"ready"`
	Steps  []doctorStep `json:"steps"`
	Fields doctorFields `json:"fields"`
}

type doctorStep struct {
	Must bool   `json:"must"`
	Text string `json:"text"`
}

type doctorPath struct {
	Path  string `json:"path"`
	State string `json:"state"`
}

type doctorFields struct {
	RepoRoot         string            `json:"repoRoot"`
	Mode             string            `json:"mode"`
	BoundTo          string            `json:"boundTo"`
	PlanktonDir      doctorPath        `json:"planktonDir"`
	NektonDir        doctorPath        `json:"nektonDir"`
	TemplatesDir     doctorPath        `json:"templatesDir"`
	PlanktonKey      doctorPath        `json:"planktonKey"`
	NektonKey        doctorPath        `json:"nektonKey"`
	AllowedTemplates []string          `json:"allowedTemplates"`
	Scopes           map[string]string `json:"scopes,omitempty"`
	TrustTiers       []string          `json:"trustTiers"`
	Persist          string            `json:"persist"`
	Anchor           bool              `json:"anchor"`
	Execution        bool              `json:"execution"`
	Kernel           map[string]string `json:"kernel"`
	Identity         doctorIdentity    `json:"identity"`
}

// doctorIdentity is who this checkout signs as (.kton/local/identity.json): its session name, the
// key ids of its two keys, and the trust tier its claim key is in — the tier its claims are
// counted in when verified, "" when no tier holds it. A key that cannot be read has no key id.
type doctorIdentity struct {
	Name          string `json:"name"`
	PlanktonKeyID string `json:"planktonKeyId,omitempty"`
	NektonKeyID   string `json:"nektonKeyId,omitempty"`
	Tier          string `json:"tier"`
	Keyring       bool   `json:"keyring"`
	// Keys are every claim key this checkout can sign as: its own nekton_key first, then each
	// <name>-claims.key of the keyring, which say --as names.
	Keys []doctorKey `json:"keys"`
}

// doctorKey is one claim key: its name (the session for the checkout's own key, the keyring name
// otherwise), its key id derived from the private seed, whether the .pub beside it is that key's
// public half (say --as refuses a key whose half does not match), and the trust tier its public half
// is in under this configuration, "" when none. A configured fact, not a verified signature.
type doctorKey struct {
	Name    string `json:"name"`
	KeyID   string `json:"keyId,omitempty"`
	Matches bool   `json:"matches"`
	Tier    string `json:"tier"`
	Problem string `json:"problem,omitempty"`
}

// claimKey describes the private key at path, named name.
func claimKey(cfg *config.Config, name, path string) doctorKey {
	k := doctorKey{Name: name}
	pubHex, err := publicOf(path)
	if err != nil {
		k.Problem = err.Error()
		return k
	}
	pub, err := core.ParsePublicKeyHex(pubHex)
	if err != nil {
		k.Problem = err.Error()
		return k
	}
	k.KeyID = core.KeyIDHex(pub)
	if b, err := os.ReadFile(strings.TrimSuffix(path, ".key") + ".pub"); err == nil {
		k.Matches = strings.EqualFold(strings.TrimSpace(string(b)), pubHex)
	}
	k.Tier, _ = verify.TierOfKey(cfg, pub)
	return k
}

func identityOf(cfg *config.Config) doctorIdentity {
	id := doctorIdentity{Name: cfg.Identity.SessionID, Keyring: cfg.Identity.Keyring != ""}
	keyID := func(path string) (string, ed25519.PublicKey) {
		pub, err := publicOf(path)
		if err != nil {
			return "", nil
		}
		p, err := core.ParsePublicKeyHex(pub)
		if err != nil {
			return "", nil
		}
		return core.KeyIDHex(p), p
	}
	id.PlanktonKeyID, _ = keyID(cfg.PlanktonKey)
	var claims ed25519.PublicKey
	id.NektonKeyID, claims = keyID(cfg.NektonKey)
	if claims != nil {
		id.Tier, _ = verify.TierOfKey(cfg, claims)
	}
	id.Keys = []doctorKey{claimKey(cfg, cfg.Identity.SessionID, cfg.NektonKey)}
	if ring := cfg.Identity.Keyring; ring != "" {
		paths, _ := filepath.Glob(filepath.Join(config.ExpandHome(ring), "*-claims.key"))
		sort.Strings(paths)
		for _, p := range paths {
			name := strings.TrimSuffix(filepath.Base(p), "-claims.key")
			if p == cfg.NektonKey {
				continue
			}
			id.Keys = append(id.Keys, claimKey(cfg, name, p))
		}
	}
	return id
}

func printDoctorJSON(ctx context.Context, cfg *config.Config) error {
	rep := doctorReportOf(ctx, cfg)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(rep); err != nil {
		return err
	}
	if !rep.Ready {
		return errNotReady
	}
	return nil
}

// doctorReportOf is what `doctor --json` prints, for the command line and the frontend protocol alike.
func doctorReportOf(ctx context.Context, cfg *config.Config) doctorReport {
	steps := readiness(ctx, cfg)
	rep := doctorReport{Ready: !blocked(steps), Steps: []doctorStep{}}
	for _, s := range steps {
		rep.Steps = append(rep.Steps, doctorStep{s.must, s.text})
	}

	raw := cfg.Raw
	f := &rep.Fields
	f.RepoRoot = cfg.RepoRoot
	f.Mode = raw.Repo.ModeOrDefault()
	if raw.Repo.IsLocal() {
		f.BoundTo = raw.Repo.Root
	} else {
		f.BoundTo = raw.Repo.Owner + "/" + raw.Repo.Name
	}
	made := func(p string) doctorPath {
		if pathState(p) != stateOK {
			return doctorPath{p, stateMade}
		}
		return doctorPath{p, stateOK}
	}
	f.PlanktonDir, f.NektonDir = made(cfg.PlanktonDir), made(cfg.NektonDir)
	f.TemplatesDir = doctorPath{cfg.TemplatesDir, pathState(cfg.TemplatesDir)}
	f.PlanktonKey = doctorPath{cfg.PlanktonKey, keyState(cfg.PlanktonKey)}
	f.NektonKey = doctorPath{cfg.NektonKey, keyState(cfg.NektonKey)}
	f.AllowedTemplates = append([]string{}, raw.Claims.AllowedTemplates...)
	f.Scopes = raw.Claims.Scopes
	f.TrustTiers = append(f.TrustTiers, raw.Trust.Tiers...)
	if f.TrustTiers == nil {
		f.TrustTiers = []string{}
	}
	sort.Strings(f.TrustTiers)
	switch {
	case raw.Repo.IsLocal():
		f.Persist = "none"
	case !raw.CommitEnabled():
		f.Persist = "off"
	case !raw.ShareEnabled():
		f.Persist = "commit"
	default:
		f.Persist = "commit+share"
	}
	f.Anchor = raw.Anchor.Enabled
	f.Execution = raw.Execution.Enabled()
	f.Kernel = map[string]string{}
	for _, mod := range linkedKernels() {
		v := mod.Version
		if mod.Replace != nil {
			v = "replaced by " + mod.Replace.Path
		}
		f.Kernel[mod.Path] = v
	}

	f.Identity = identityOf(cfg)
	return rep
}

func pathState(p string) string {
	if _, err := os.Stat(p); err != nil {
		return stateMissing
	}
	return stateOK
}

func keyState(p string) string {
	fi, err := os.Stat(p)
	if err != nil {
		return stateMissing
	}
	if keyModeExposed(fi.Mode().Perm()) {
		return stateExposed
	}
	return stateOK
}

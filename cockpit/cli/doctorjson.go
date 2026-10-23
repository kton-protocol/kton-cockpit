package cli

// doctorjson.go is `doctor --json`: the same readiness as the text, for a program to read rather
// than parse. The fields are the configured core a frontend shows beside the badge; everything the
// text explains at length (scopes, material, anchor detail) stays in the text.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"

	"github.com/kton-protocol/kton-cockpit/internal/config"
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
}

func printDoctorJSON(ctx context.Context, cfg *config.Config) error {
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
	for tier := range raw.Trust.Tiers {
		f.TrustTiers = append(f.TrustTiers, tier)
	}
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

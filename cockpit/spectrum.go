package cockpit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gitmick/ktonpkg"
	"github.com/gitmick/ktonpkg/scope"
	"kton.dev/plankton/core"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
	"github.com/kton-protocol/kton-cockpit/internal/packages"
)

// subjectEnvironment is the environment class the run being reproduced recorded: the subject's own
// descriptor.environment when the subject is a foton, and otherwise the one class every recorded
// producer of the subject's output agrees on. Empty when there is none, or when the producers name
// different classes, since then there is no one normaliser the comparison could be judged through.
func subjectEnvironment(ctx context.Context, r *binaries.Runner, in SayRequest) string {
	if f, err := r.Foton(ctx, in.Subject); err == nil && f != nil {
		return descriptorEnvironment(f)
	}
	prod, err := r.Producer(ctx, in.SubjectOutputHash)
	if err != nil || prod == nil {
		return ""
	}
	class := ""
	for _, rec := range prod.Records {
		f, err := r.Foton(ctx, rec.ID)
		if err != nil || f == nil {
			continue
		}
		c := descriptorEnvironment(f)
		if c == "" {
			continue
		}
		if class != "" && c != class {
			return ""
		}
		class = c
	}
	return class
}

func descriptorEnvironment(f *core.Foton) string {
	c, _ := f.Protocol.Descriptor["environment"].(string)
	return c
}

// spectrumNormalizer finds the spectrum named class among the packages installed in this repo and
// returns the normaliser potential it names, with the package it came from. A spectrum is named by
// its id, which is either the hash of its manifest's bytes or ktonpkg's id of its canonical form;
// both are checked against the manifest itself rather than taken from the package's claim, so a
// package cannot file one spectrum's normaliser under another's id.
//
// It reads from each package's latest sealed revision only, as templates and queries are read, and
// writes nothing. An empty potential means no installed package carries that spectrum, or the one
// that does names no normaliser.
func spectrumNormalizer(cfg *config.Config, class string) (potential, from string, err error) {
	if class == "" {
		return "", "", nil
	}
	all, err := packages.List(cfg.RepoRoot)
	if err != nil {
		return "", "", err
	}
	for _, inst := range all {
		claims, err := inst.Revision.Claims()
		if err != nil {
			return "", "", fmt.Errorf("%s: %w", inst.Dir, err)
		}
		for _, c := range claims {
			v, _ := c.Object["value"].(map[string]any)
			if c.Predicate != scope.PartOf || v["role"] != scope.RoleSpectrum {
				continue
			}
			raw, err := inst.Package.Blob(c.Subject)
			if err != nil {
				return "", "", fmt.Errorf("%s: the spectrum %s: %w", inst.Dir, c.Subject, err)
			}
			var s ktonpkg.Spectrum
			if err := json.Unmarshal(raw, &s); err != nil {
				return "", "", fmt.Errorf("%s: the spectrum %s: %w", inst.Dir, c.Subject, err)
			}
			id, _ := s.ID()
			if core.HashBytes(raw) != class && id != class {
				continue
			}
			return s.Normalizer, inst.Dir, nil
		}
	}
	return "", "", nil
}

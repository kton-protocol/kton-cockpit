package cli

// pin.go is `cockpit pin <image>`: the image publish and run execute in, pinned by digest.
//
// The configuration takes only oci://<repo>@sha256:<digest>, because a tag names whatever it points
// at today. Finding that digest was `docker image inspect`, choosing among RepoDigests, and adding
// the scheme by hand. This does those three steps and writes execution.image — nothing else.
//
//	cockpit pin python:3.12-slim      pull if absent, resolve, write execution.image
//	cockpit pin                       what is pinned now

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kton-protocol/kton-cockpit/internal/config"
)

func runPin(ctx context.Context, args []string) error {
	cfg, err := config.Load(ctx, ".")
	if err != nil {
		return err
	}
	if len(args) == 0 {
		if cfg.Raw.Execution.Image == "" {
			fmt.Println("nothing pinned — publish records commands without running them. Pin with: cockpit pin <image>")
		} else {
			fmt.Println(cfg.Raw.Execution.Image)
		}
		return nil
	}
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return fmt.Errorf("usage: cockpit pin <image>      (e.g. cockpit pin python:3.12-slim)")
	}
	engine := cfg.Raw.Execution.Engine
	if engine == "" {
		engine = "docker"
	}

	ref, err := resolveDigest(ctx, engine, strings.TrimPrefix(args[0], "oci://"))
	if err != nil {
		return err
	}
	pinned := "oci://" + ref
	if pinned == cfg.Raw.Execution.Image {
		fmt.Printf("already pinned: %s\n", pinned)
		return nil
	}
	if err := editConfig(filepath.Join(cfg.RepoRoot, "cockpit.config.json"), set([]string{"execution", "image"}, pinned)); err != nil {
		return err
	}
	if cfg.Raw.Execution.Image != "" {
		fmt.Printf("was:    %s\n", cfg.Raw.Execution.Image)
	}
	fmt.Printf("pinned: %s\n", pinned)
	fmt.Println("  publish and run now execute in exactly this image; commit cockpit.config.json so a peer runs in it too")
	return nil
}

// resolveDigest returns <repo>@sha256:<digest> for an image, pulling it when it is not present.
func resolveDigest(ctx context.Context, engine, image string) (string, error) {
	name := repoOf(image)
	if strings.Contains(image, "@sha256:") {
		return image, nil
	}
	digests, err := repoDigests(ctx, engine, image)
	if err != nil {
		fmt.Printf("pulling %s ...\n", image)
		if out, perr := exec.CommandContext(ctx, engine, "pull", "--quiet", image).CombinedOutput(); perr != nil {
			return "", fmt.Errorf("%s pull %s: %v\n%s", engine, image, perr, strings.TrimSpace(string(out)))
		}
		if digests, err = repoDigests(ctx, engine, image); err != nil {
			return "", err
		}
	}
	if len(digests) == 0 {
		return "", fmt.Errorf("%s has no registry digest: it was built here and never pushed or pulled. "+
			"A peer cannot fetch it by digest, so it pins nothing they can use — push it to a registry, then pin it", image)
	}
	// Chosen by name, not by position: an image pushed to several repositories has a digest per
	// repository, and which one comes first is an accident of history.
	var match []string
	for _, d := range digests {
		if repoOf(d) == name || repoOf(d) == "docker.io/library/"+name || "docker.io/library/"+repoOf(d) == name {
			match = append(match, d)
		}
	}
	if len(match) != 1 {
		return "", fmt.Errorf("%s has %d registry digests and %d for the name %s: %s — pin one of them by its full reference",
			image, len(digests), len(match), name, strings.Join(digests, ", "))
	}
	return match[0], nil
}

func repoDigests(ctx context.Context, engine, image string) ([]string, error) {
	out, err := exec.CommandContext(ctx, engine, "image", "inspect", image, "--format", "{{json .RepoDigests}}").Output()
	if err != nil {
		return nil, fmt.Errorf("%s image inspect %s: %w", engine, image, err)
	}
	var ds []string
	if err := json.Unmarshal(out, &ds); err != nil {
		return nil, fmt.Errorf("%s image inspect %s: %w", engine, image, err)
	}
	return ds, nil
}

// repoOf is an image reference without its tag or digest: registry/path/name.
func repoOf(ref string) string {
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i]
	}
	return ref
}

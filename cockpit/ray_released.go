//go:build !unreleased

package cockpit

import (
	"context"

	"github.com/kton-protocol/kton-cockpit/internal/binaries"
	"github.com/kton-protocol/kton-cockpit/internal/config"
)

// A potential from executions (ADR-006) is proposed and extracted by ktonpkg, which is not
// published. This build is against published modules only: ask {query: "ray"} and publish
// {kind: "ray"} refuse, and the request and result shapes stay so callers compile unchanged.

type (
	rayProposal  struct{}
	rayExecution struct{}
)

// RayPublish is the ray half of a publish request.
type RayPublish struct {
	Refs   []string       `json:"refs" jsonschema:"the endpoints of the runs to extract from: output hashes or repo-relative files"`
	Choice map[string]any `json:"choice" jsonschema:"what was chosen from the proposal of ask {query: ray}"`
	Dir    string         `json:"dir" jsonschema:"repo-relative directory for the package; must not exist yet"`
}

// RayResult reports a published ray.
type RayResult struct {
	Dir         string   `json:"dir"`
	RayID       string   `json:"rayId"`
	BundleID    string   `json:"bundleId"`
	DerivedFrom []string `json:"derivedFrom"`
	ClaimID     string   `json:"claimId"`
}

func errRayUnreleased() error {
	return refuse("ray.unavailable", "", "a ray needs ktonpkg, which is not published; this cockpit is built without it (build with -tags unreleased)")
}

func askRay(context.Context, *config.Config, *binaries.Runner, AskRequest, *AskFilter) (*AskResult, error) {
	return nil, errRayUnreleased()
}

func (c *Cockpit) publishRay(context.Context, *config.Config, PublishRequest) (*PublishResult, error) {
	return nil, errRayUnreleased()
}

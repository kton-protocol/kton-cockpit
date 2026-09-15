package tools

// scope_query.go is ask's adapter onto internal/scope: the seal verdict is computed there because
// `cockpit scope read` needs the same answer about a file somebody sent, and two implementations of
// "is this chain whole" would disagree in exactly the cases that matter.

import (
	"context"

	"github.com/deathbychoco/claude-science-cockpit/internal/binaries"
	"github.com/deathbychoco/claude-science-cockpit/internal/config"
	"github.com/deathbychoco/claude-science-cockpit/internal/scope"
	"github.com/deathbychoco/claude-science-cockpit/internal/verify"
)

// SealVerdict is what a reader needs before relying on a scope. Aliased rather than redefined so
// the wire shape a session sees and the one an operator reads are the same object.
type SealVerdict = scope.Verdict

// askScope verifies a scope's seal and files its claims into the answer under the usual rule: a
// claim that no configured key verifies is reported as found-and-excluded, never assembled in.
func askScope(ctx context.Context, cfg *config.Config, r *binaries.Runner, scopeID string, out *AskOutput) (*SealVerdict, string) {
	verdict, claims, err := scope.Describe(ctx, cfg, r, scopeID)
	if err != nil {
		return nil, err.Error()
	}
	for _, c := range claims {
		trusted := c.Tier != verify.Untrusted
		out.Records = append(out.Records, RecordVerification{ID: c.Claim.ID, Tier: c.Tier, Verified: trusted})
		if !trusted {
			out.Excluded = append(out.Excluded, c.Claim.ID)
			continue
		}
		out.Included = append(out.Included, c.Claim.ID)
		out.Claims = append(out.Claims, c.Claim)
	}
	return verdict, ""
}

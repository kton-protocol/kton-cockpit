package binaries

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// This file reads the claim axis out of what `nekton about --json` / `nekton by --json` return.
// It adds no protocol logic: the envelope is not verified here (that stays with `nekton verify`,
// via the verify package), nothing is canonicalized or hashed, and no id is recomputed. It only
// base64-decodes the payload the kernel just handed back and reads documented fields out of it —
// the same fields nekton's own prose rendering reads, plus the ones prose has no room for.
//
// Before --json (upstream #39) the only machine-readable surface was that prose, so a consumer
// could see THAT a claim existed but not WHAT it said: the object — the actual values a template's
// fields were filled with — never appeared at all.

// ClaimAxis is one claim as the cockpit surfaces it.
//
// DeclaredBy is named for what it is. The `by` field is the signer the claim declares about
// itself, and it is never evidence of anything: trust is resolved separately by verifying the
// signature against the pubkeys in the configured trust tiers (see the verify package). Prose
// output marks this "(unverified)" for the same reason.
type ClaimAxis struct {
	ID              string   `json:"id"`
	Subject         string   `json:"subject,omitempty"`
	PredicateType   string   `json:"predicateType,omitempty"`
	Predicate       string   `json:"predicate,omitempty"`
	Object          any      `json:"object,omitempty"`
	DeclaredBy      string   `json:"declaredBy,omitempty"`
	When            string   `json:"when,omitempty"`
	Why             string   `json:"why,omitempty"`
	Context         string   `json:"context,omitempty"`
	Scope           string   `json:"scope,omitempty"`
	Prev            string   `json:"prev,omitempty"`
	SignatureKeyIDs []string `json:"signatureKeyIds,omitempty"`
}

// Line renders one claim as a single human-readable line, for AskOutput.Raw.
func (c ClaimAxis) Line() string {
	var b strings.Builder
	b.WriteString(c.ID)
	if c.Predicate != "" {
		b.WriteString("  predicate=" + c.Predicate)
	}
	if c.Object != nil {
		if o, err := json.Marshal(c.Object); err == nil {
			b.WriteString("  object=" + string(o))
		}
	}
	if c.When != "" {
		b.WriteString("  when=" + c.When)
	}
	if c.DeclaredBy != "" {
		b.WriteString("  declared-by=" + c.DeclaredBy)
	}
	return b.String()
}

type claimEnvelopeJSON struct {
	ClaimID  string `json:"claimId"`
	Envelope struct {
		Payload    string `json:"payload"`
		Signatures []struct {
			KeyID string `json:"keyid"`
		} `json:"signatures"`
	} `json:"envelope"`
}

// statementJSON is the in-toto statement inside the DSSE payload. Predicate stays raw: the claim
// body's own shape is vocabulary, not protocol, and only the handful of fields below are read.
type statementJSON struct {
	Subject []struct {
		Digest map[string]string `json:"digest"`
		URI    string            `json:"uri,omitempty"`
	} `json:"subject"`
	PredicateType string          `json:"predicateType"`
	Predicate     json.RawMessage `json:"predicate"`
}

// predicateJSON is the claim body. Note the name collision the protocol itself carries: the
// statement's `predicateType` (which KIND of statement this is) and this nested `predicate` (WHAT
// is asserted) are two different things that happen to share a word.
type predicateJSON struct {
	Predicate termRefJSON     `json:"predicate"`
	Object    json.RawMessage `json:"object,omitempty"`
	Context   *termRefJSON    `json:"context,omitempty"`
	By        string          `json:"by"`
	When      string          `json:"when"`
	Why       string          `json:"why,omitempty"`
	Scope     string          `json:"scope,omitempty"`
	Prev      string          `json:"prev,omitempty"`
}

// termRefJSON is a reference to a term: a URI, or a content hash when the term is itself a
// registered record rather than a well-known IRI.
type termRefJSON struct {
	Hash string `json:"hash,omitempty"`
	URI  string `json:"uri,omitempty"`
}

func (t termRefJSON) key() string {
	if t.Hash != "" {
		return t.Hash
	}
	return t.URI
}

// parseClaimsJSON decodes what nekton's --json mode prints into one ClaimAxis per claim. An empty
// result set prints "[]", which is a valid, meaningful answer — not an error.
func parseClaimsJSON(out string) ([]ClaimAxis, error) {
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, nil
	}
	var envs []claimEnvelopeJSON
	if err := json.Unmarshal([]byte(out), &envs); err != nil {
		return nil, fmt.Errorf("could not read nekton's --json output: %w", err)
	}

	claims := make([]ClaimAxis, 0, len(envs))
	for _, e := range envs {
		c := ClaimAxis{ID: e.ClaimID}
		for _, s := range e.Envelope.Signatures {
			c.SignatureKeyIDs = append(c.SignatureKeyIDs, s.KeyID)
		}

		// A payload that will not decode is reported as such rather than dropped: a claim the
		// cockpit cannot read must not silently vanish from an answer about what was said.
		pb, err := base64.StdEncoding.DecodeString(e.Envelope.Payload)
		if err != nil {
			return nil, fmt.Errorf("claim %s: payload is not valid base64: %w", e.ClaimID, err)
		}
		var st statementJSON
		if err := json.Unmarshal(pb, &st); err != nil {
			return nil, fmt.Errorf("claim %s: payload is not a readable statement: %w", e.ClaimID, err)
		}
		c.PredicateType = st.PredicateType
		if len(st.Subject) > 0 {
			if h := st.Subject[0].Digest["sha256"]; h != "" {
				c.Subject = "sha256:" + h
			} else {
				c.Subject = st.Subject[0].URI
			}
		}

		var p predicateJSON
		if err := json.Unmarshal(st.Predicate, &p); err != nil {
			return nil, fmt.Errorf("claim %s: claim body is not readable: %w", e.ClaimID, err)
		}
		c.Predicate = p.Predicate.key()
		c.DeclaredBy, c.When, c.Why = p.By, p.When, p.Why
		c.Scope, c.Prev = p.Scope, p.Prev
		if p.Context != nil {
			c.Context = p.Context.key()
		}
		if len(p.Object) > 0 {
			if err := json.Unmarshal(p.Object, &c.Object); err != nil {
				return nil, fmt.Errorf("claim %s: object is not readable: %w", e.ClaimID, err)
			}
		}
		claims = append(claims, c)
	}
	return claims, nil
}

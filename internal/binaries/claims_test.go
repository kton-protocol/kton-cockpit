package binaries

import "testing"

// A real envelope as `nekton about --json` emits it: a working-on claim, payload base64-encoded.
const oneClaimJSON = `[
  {
    "claimId": "sha256:837e433771be41bb43a5713055355ebed22099961a225ef113793ce8fae309ec",
    "envelope": {
      "payloadType": "application/vnd.in-toto+json",
      "payload": "eyJfdHlwZSI6Imh0dHBzOi8vaW4tdG90by5pby9TdGF0ZW1lbnQvdjEiLCJwcmVkaWNhdGUiOnsiYnkiOiJrZXk6ZGE0YjcwMWRkNzBlMjQ1NSIsIm9iamVjdCI6eyJieS1zZXNzaW9uIjoiczEiLCJzdGVwIjoiYW5hbHlzaXMifSwicHJlZGljYXRlIjp7InVyaSI6Imh0dHBzOi8va3Rvbi5kZXYvdi93b3JraW5nLW9uIn0sIndoZW4iOiIyMDI2LTA5LTAxVDE1OjA2OjIwWiJ9LCJwcmVkaWNhdGVUeXBlIjoiaHR0cHM6Ly9rdG9uLmRldi9jbGFpbS92MCIsInN1YmplY3QiOlt7ImRpZ2VzdCI6eyJzaGEyNTYiOiIyZDcxMTY0MmI3MjZiMDQ0MDE2MjdjYTlmYmFjMzJmNWM4NTMwZmIxOTAzY2M0ZGIwMjI1ODcxNzkyMWE0ODgxIn19XX0=",
      "signatures": [{"keyid": "da4b701dd70e2455", "sig": "DqQeAR9V0J7reUltCvQmxR8t1GkTqWNuZBs1fVIXy1mWOHxzaLKoki8LrywZKnHGY9hoqDwhsZqAw2EwS9F2DA=="}]
    }
  }
]`

func TestParseClaimsJSON_DecodesTheWholeAxis(t *testing.T) {
	claims, err := parseClaimsJSON(oneClaimJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 1 {
		t.Fatalf("expected 1 claim, got %d", len(claims))
	}
	c := claims[0]
	if c.Predicate != "https://kton.dev/v/working-on" {
		t.Errorf("predicate: %q", c.Predicate)
	}
	if c.PredicateType != "https://kton.dev/claim/v0" {
		t.Errorf("predicateType: %q", c.PredicateType)
	}
	if c.Subject != "sha256:2d711642b726b04401627ca9fbac32f5c8530fb1903cc4db02258717921a4881" {
		t.Errorf("subject: %q", c.Subject)
	}
	if c.DeclaredBy != "key:da4b701dd70e2455" {
		t.Errorf("declaredBy: %q", c.DeclaredBy)
	}
	if c.When != "2026-09-01T15:06:20Z" {
		t.Errorf("when: %q", c.When)
	}
	if len(c.SignatureKeyIDs) != 1 || c.SignatureKeyIDs[0] != "da4b701dd70e2455" {
		t.Errorf("signatureKeyIds: %+v", c.SignatureKeyIDs)
	}
	obj, ok := c.Object.(map[string]any)
	if !ok || obj["step"] != "analysis" || obj["by-session"] != "s1" {
		t.Errorf("object: %#v", c.Object)
	}
}

// An empty result set is a meaningful answer, not a failure.
func TestParseClaimsJSON_EmptyArrayIsNotAnError(t *testing.T) {
	claims, err := parseClaimsJSON("[]")
	if err != nil {
		t.Fatal(err)
	}
	if len(claims) != 0 {
		t.Fatalf("expected no claims, got %+v", claims)
	}
}

// A claim whose payload cannot be read must surface as an error rather than be quietly dropped:
// silently returning fewer claims than the registry holds would understate what was said, and the
// caller has no way to notice.
func TestParseClaimsJSON_UnreadablePayloadFailsRatherThanDroppingTheClaim(t *testing.T) {
	for name, in := range map[string]string{
		"not base64":      `[{"claimId":"sha256:aa","envelope":{"payload":"!!!not base64!!!","signatures":[]}}]`,
		"not a statement": `[{"claimId":"sha256:aa","envelope":{"payload":"bm90IGpzb24=","signatures":[]}}]`,
		"not an array":    `{"claimId":"sha256:aa"}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseClaimsJSON(in); err == nil {
				t.Fatal("expected an error, got none")
			}
		})
	}
}

package binaries

import "testing"

// The lineage decoder is gone: producer/uses/lineage now read the registry directly, so there is
// no wire form between this cockpit and the answer. What those fixtures protected — that a record's
// id comes from a named field rather than from guessing at text — the registry gives by returning
// ids as []string, in its own order, which the CLI's JSON form could no longer convey at all.

// "Nobody else produced these bytes" and "somebody did, but nobody this repo trusts" are different
// answers, and the second must not read as the first.
func TestParseReproductionsJSON_KeepsTheUntrustedExclusionCount(t *testing.T) {
	const in = `{
      "distinctSigners": 0,
      "excludedUntrusted": 1,
      "output": "sha256:3e23e8160039594a33894f6564e1b1348bbd7a0088d42c4acb73eeaed59c009d",
      "producerFotons": 1,
      "producers": [],
      "trust": "verified"
    }`
	res, err := parseReproductionsJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	if res.DistinctSigners != 0 || res.ProducerFotons != 1 || res.ExcludedUntrusted != 1 {
		t.Fatalf("counts not decoded: %+v", res)
	}
	if res.Trust != "verified" {
		t.Errorf("trust: %q", res.Trust)
	}
}

func TestParseReproductionsJSON_DecodesEachVerifiedProducer(t *testing.T) {
	const in = `{
      "distinctSigners": 1, "output": "sha256:aa", "producerFotons": 1, "trust": "verified",
      "producers": [{"fotonId": "sha256:bb", "keyid": "b9e41674a7678123", "verified": true}]
    }`
	res, err := parseReproductionsJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Producers) != 1 {
		t.Fatalf("expected 1 producer, got %+v", res.Producers)
	}
	p := res.Producers[0]
	if p.ID != "sha256:bb" || p.KeyID != "b9e41674a7678123" || !p.Verified {
		t.Errorf("producer not decoded: %+v", p)
	}
}

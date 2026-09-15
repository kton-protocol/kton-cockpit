package binaries

import "testing"

// The wire form kton §12 declares: `records` is an array of bare ENVELOPES, and the per-record
// detail sits beside it in `summary`, keyed by foton id. It was once one array of summary objects
// with the envelope nested inside, which meant a consumer decoding the declared shape got an array
// of things that were not envelopes.
const producerJSON = `{
  "query": "sha256:0263829989b6fd954f72baaf2fc64bc2e2f01d692d4de72986ea808f6e99813f",
  "records": [
    {"payloadType": "application/vnd.in-toto+json", "payload": "e30=", "signatures": []}
  ],
  "summary": {
    "sha256:d9eb88fb38c71c992cd35bc1d834d8f9bda17d00481d6c986ab11f97fedb3377":
      {"inputs": 1, "kind": "script", "outputs": 1}
  },
  "relation": "producer"
}`

// A record's id is stated ONLY in `summary` now, so an answer whose two halves disagree cannot be
// read record by record. Decoding it as zero records would hand on "nothing found" — an empty
// answer wearing the shape of a finding.
func TestParseLineageJSON_RefusesAnAnswerWhoseHalvesDisagree(t *testing.T) {
	_, err := parseLineageJSON(`{"relation":"producer","query":"x",
	  "records":[{"payloadType":"application/vnd.in-toto+json","payload":"e30=","signatures":[]}],
	  "summary":{}}`)
	if err == nil {
		t.Fatal("an answer with one record and no summary was decoded as an empty result")
	}
}

func TestParseLineageJSON_TakesTheIdFromItsNamedField(t *testing.T) {
	res, err := parseLineageJSON(producerJSON)
	if err != nil {
		t.Fatal(err)
	}
	if res.Relation != "producer" {
		t.Errorf("relation: %q", res.Relation)
	}
	if len(res.Records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(res.Records))
	}
	r := res.Records[0]
	if r.ID != "sha256:d9eb88fb38c71c992cd35bc1d834d8f9bda17d00481d6c986ab11f97fedb3377" {
		t.Errorf("id: %q", r.ID)
	}
	if r.Kind != "script" || r.Inputs != 1 || r.Outputs != 1 {
		t.Errorf("record not decoded: %+v", r)
	}
	// The query subject is its own field, so it can never be mistaken for a found record — the
	// thing the old text-scraping path had to filter out by hand.
	if res.Query == r.ID {
		t.Error("query subject and record id must be separate fields")
	}
}

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

func TestParseLineageJSON_MalformedOutputIsAnError(t *testing.T) {
	if _, err := parseLineageJSON("not json"); err == nil {
		t.Fatal("expected an error")
	}
}

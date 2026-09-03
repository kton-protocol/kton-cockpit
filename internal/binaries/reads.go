package binaries

import (
	"encoding/json"
	"fmt"
	"strings"
)

// This file reads plankton's --json mode for the four lineage queries (kton #57). Before it, the
// only surface was text, and the cockpit had to pull `sha256:` ids out of it with a regex and then
// redact whole LINES to keep unverified records from reaching the model — which worked only under
// an assumption nothing guaranteed: that a record's own id is the FIRST hash on its line. plankton
// now says the opposite in its own usage text: "A record's id is a NAMED field there, so a consumer
// never has to assume it is the first hash on a line."
//
// Nothing here verifies anything. Trust is still resolved per record by the verify package, which
// runs the binaries' own verify against the configured trust tiers.

// Record is one stored record: the id the substrate filed it under, and the signed envelope
// itself. `{seq, fotonId|claimId, envelope}` is what the kernel persists, what `add` accepts, and
// byte-for-byte the SPEC §12 sync(since) answer — so this is that query, answered over stdout.
//
// The envelope is kept raw. Anything that must CHECK a record — a viewer re-verifying in the
// browser, `kton anchor` handing it to Rekor — needs the bytes as signed, and a struct that decoded
// and re-encoded them would be handing on something re-serialised rather than what was signed.
type Record struct {
	Seq      int             `json:"seq"`
	FotonID  string          `json:"fotonId,omitempty"`
	ClaimID  string          `json:"claimId,omitempty"`
	Envelope json.RawMessage `json:"envelope"`
}

// ID is whichever id this record carries.
func (r Record) ID() string {
	if r.FotonID != "" {
		return r.FotonID
	}
	return r.ClaimID
}

type recordsJSON struct {
	Max     int      `json:"max"`
	Records []Record `json:"records"`
}

// FotonRecord is one foton as the lineage queries report it.
type FotonRecord struct {
	ID      string `json:"id"`
	Kind    string `json:"kind,omitempty"`
	Inputs  int    `json:"inputs"`
	Outputs int    `json:"outputs"`
}

// Line renders one record for the human-readable Raw field.
func (f FotonRecord) Line() string {
	var b strings.Builder
	b.WriteString(f.ID)
	if f.Kind != "" {
		b.WriteString("  kind=" + f.Kind)
	}
	fmt.Fprintf(&b, "  in=%d out=%d", f.Inputs, f.Outputs)
	return b.String()
}

// LineageResult is what producer/uses/lineage answer.
type LineageResult struct {
	Relation string
	Query    string
	Records  []FotonRecord
	// Warning carries success-path stderr — plankton can warn that a registry read was degraded
	// ("N record(s) skipped on load - this read is INCOMPLETE") while still exiting cleanly. An
	// incomplete read that reports itself as a complete answer is exactly the failure this project
	// exists to prevent, so it is surfaced rather than discarded.
	Warning string
}

// ReproductionProducer is one independent producer of a set of output bytes.
type ReproductionProducer struct {
	ID       string `json:"id"`
	KeyID    string `json:"keyid"`
	Verified bool   `json:"verified"`
}

// ReproductionsResult is plankton's ↻N answer.
type ReproductionsResult struct {
	Output          string `json:"output"`
	DistinctSigners int    `json:"distinctSigners"`
	ProducerFotons  int    `json:"producerFotons"`
	// ExcludedUntrusted is how many producer fotons were dropped from the count because no trusted
	// key verified them. Carried because the difference between "nobody else produced these bytes"
	// and "somebody did, but not anyone this repo trusts" is the whole point of the count.
	ExcludedUntrusted int                    `json:"excludedUntrusted"`
	Trust             string                 `json:"trust"`
	Producers         []ReproductionProducer `json:"producers"`
	Warning           string                 `json:"-"`
}

// wire shapes, named as plankton prints them.
type lineageJSON struct {
	Query    string `json:"query"`
	Relation string `json:"relation"`
	Records  []struct {
		FotonID string `json:"fotonId"`
		Kind    string `json:"kind"`
		Inputs  int    `json:"inputs"`
		Outputs int    `json:"outputs"`
	} `json:"records"`
}

type reproductionsJSON struct {
	Output            string `json:"output"`
	DistinctSigners   int    `json:"distinctSigners"`
	ProducerFotons    int    `json:"producerFotons"`
	ExcludedUntrusted int    `json:"excludedUntrusted"`
	Trust             string `json:"trust"`
	Producers         []struct {
		FotonID  string `json:"fotonId"`
		KeyID    string `json:"keyid"`
		Verified bool   `json:"verified"`
	} `json:"producers"`
}

func parseRecordsJSON(out string) ([]Record, error) {
	var w recordsJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &w); err != nil {
		return nil, fmt.Errorf("could not read the records: %w", err)
	}
	for _, rec := range w.Records {
		// A record with no envelope is not a record anything can verify, and silently passing it on
		// would put it in a union a viewer then draws without being able to check it.
		if len(rec.Envelope) == 0 {
			return nil, fmt.Errorf("record %s came back without an envelope", rec.ID())
		}
	}
	return w.Records, nil
}

func parseLineageJSON(out string) (*LineageResult, error) {
	var w lineageJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &w); err != nil {
		return nil, fmt.Errorf("could not read plankton's --json output: %w", err)
	}
	res := &LineageResult{Relation: w.Relation, Query: w.Query}
	for _, r := range w.Records {
		res.Records = append(res.Records, FotonRecord{ID: r.FotonID, Kind: r.Kind, Inputs: r.Inputs, Outputs: r.Outputs})
	}
	return res, nil
}

func parseReproductionsJSON(out string) (*ReproductionsResult, error) {
	var w reproductionsJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &w); err != nil {
		return nil, fmt.Errorf("could not read plankton's --json output: %w", err)
	}
	res := &ReproductionsResult{
		Output:            w.Output,
		DistinctSigners:   w.DistinctSigners,
		ProducerFotons:    w.ProducerFotons,
		ExcludedUntrusted: w.ExcludedUntrusted,
		Trust:             w.Trust,
	}
	for _, p := range w.Producers {
		res.Producers = append(res.Producers, ReproductionProducer{ID: p.FotonID, KeyID: p.KeyID, Verified: p.Verified})
	}
	return res, nil
}

package anchor

import "testing"

// What `kton anchor` actually prints: two human lines, then the entry, on one stream. The entry is
// found by its opening brace rather than by line position, so a third human line does not silently
// shift the parse.
const anchorOutput = `anchored in Rekor: logIndex=12345678  uuid=24296fb24b8ad77a9f
  inclusion proof + SET verified against Rekor's public key (independent witness)
{
  "logIndex": 12345678,
  "uuid": "24296fb24b8ad77a9f",
  "body": "eyJhcGlWZXJzaW9uIjoiMC4wLjEifQ==",
  "integratedTime": 1788000000
}
`

func TestParseAnchorOutput_FindsTheEntryPastTheProse(t *testing.T) {
	raw, e, err := parseAnchorOutput(anchorOutput)
	if err != nil {
		t.Fatal(err)
	}
	if e.LogIndex != 12345678 || e.UUID != "24296fb24b8ad77a9f" {
		t.Fatalf("coordinates not read: %+v", e)
	}
	// The whole entry is kept for the sidecar, not just the two fields reported back: the proof is
	// the point, and a caller who only stored the coordinates would have stored nothing verifiable.
	if len(raw) < len(`{"logIndex":1,"uuid":"x"}`) {
		t.Fatalf("the stored entry is too small to be the proof: %s", raw)
	}
}

// An extra human line must not break the parse — the whole reason the brace is searched for.
func TestParseAnchorOutput_SurvivesAnExtraProseLine(t *testing.T) {
	if _, _, err := parseAnchorOutput("note: something\n" + anchorOutput); err != nil {
		t.Fatal(err)
	}
}

// Failing closed matters more here than elsewhere: an anchor that was not really recorded, reported
// as if it were, is a witness that does not exist.
func TestParseAnchorOutput_RefusesOutputWithNoEntry(t *testing.T) {
	for name, in := range map[string]string{
		"only prose":         "anchored in Rekor: logIndex=1 uuid=x\n",
		"not json":           "anchored\n{ this is not json }\n",
		"entry with no uuid": "anchored\n{\"logIndex\": 5}\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseAnchorOutput(in); err == nil {
				t.Fatal("expected an error, got none")
			}
		})
	}
}

func TestTrimKeySuffix_FindsThePublicHalfBesideThePrivateOne(t *testing.T) {
	if got := trimKeySuffix("/repo/keys/session-1.key.pub"); got != "/repo/keys/session-1.pub" {
		t.Fatalf("got %q", got)
	}
}

#!/usr/bin/env bash
# 09 - carried evidence: who a signing key belongs to, and the difference between
#      "we checked it" and "we are carrying it".
#
# A record says WHICH key signed it (05). It does not say whose key that is. kton §8.1 leaves a
# place for evidence about a record — opaque bytes under a scheme the kernel never interprets — and
# it deliberately accepts a scheme it has never heard of, because refusing unknown evidence would
# turn the list of schemes into a protocol version.
#
# The cockpit's side of that: attach anything configured, evaluate whatever it can actually
# evaluate, and report per item which of the two happened. This example shows all three verdicts.
cd "$(dirname "$0")"
source ../lib/common.sh
WORK="$PWD/.work"; participant "$WORK/repo"; cd "$WORK/repo"

echo ""
echo "== issuing a certificate FOR THE KEY PLANKTON ALREADY HAS =="
# The operator's real question is this one, so it is done here rather than assumed. plankton stores
# an ed25519 public key as bare hex; a certificate needs it as a SubjectPublicKeyInfo. For ed25519
# that structure is a fixed 12-byte prefix followed by the 32 key bytes, so the conversion is a
# concatenation and nothing is being re-derived or re-signed.
mkdir -p identity
openssl genpkey -algorithm ed25519 -out identity/ca.key 2>/dev/null
openssl req -x509 -new -key identity/ca.key -subj "/CN=Cockpit Examples Root" -days 1 \
  -out identity/root.pem 2>/dev/null

printf '302a300506032b6570032100%s' "$(cat keys/session-1.pub)" | xxd -r -p > identity/signer.spki.der
openssl pkey -pubin -inform DER -in identity/signer.spki.der -out identity/signer.spki.pem 2>/dev/null
# -force_pubkey is what makes this a certificate ABOUT plankton's key: the CSR is only a carrier for
# the subject name, and the key that ends up in the certificate is the one named here.
openssl req -new -key identity/ca.key -subj "/CN=Jane Researcher" -out identity/dummy.csr 2>/dev/null
openssl x509 -req -in identity/dummy.csr -CA identity/root.pem -CAkey identity/ca.key \
  -force_pubkey identity/signer.spki.pem -days 1 -out identity/signer.pem 2>/dev/null
rm -f identity/dummy.csr identity/signer.spki.der identity/signer.spki.pem
echo "  identity/signer.pem: CN=Jane Researcher, issued for keys/session-1.pub"

# A certificate for somebody else's key — a perfectly valid one, from the same root.
openssl genpkey -algorithm ed25519 -out identity/other.key 2>/dev/null
openssl req -new -key identity/other.key -subj "/CN=Somebody Else" -out identity/other.csr 2>/dev/null
openssl x509 -req -in identity/other.csr -CA identity/root.pem -CAkey identity/ca.key \
  -days 1 -out identity/somebody-else.pem 2>/dev/null
rm -f identity/other.csr identity/other.key
echo "  identity/somebody-else.pem: CN=Somebody Else, valid, and about a different key"

# Evidence in a format nothing here can read. It is attached anyway; that is the point.
printf 'ACME-BADGE-v3\x01\x02\x03not-a-format-anyone-here-knows\n' > identity/house-badge.bin

configure() {
  python3 - cockpit.config.json "$1" <<'PY'
import json, sys
path, mode = sys.argv[1], sys.argv[2]
cfg = json.load(open(path))
cert = {"scheme": "x509-cert", "mediaType": "application/x-pem-file", "file": "identity/signer.pem"}
badge = {"scheme": "acme-internal-badge-v3", "mediaType": "application/octet-stream",
         "file": "identity/house-badge.bin"}
if mode == "verified":
    cfg["material"] = {"attach": [cert, badge], "x509Roots": ["identity/root.pem"]}
elif mode == "no-roots":
    cfg["material"] = {"attach": [cert]}
elif mode == "wrong-key":
    cfg["material"] = {"attach": [dict(cert, file="identity/somebody-else.pem")],
                       "x509Roots": ["identity/root.pem"]}
json.dump(cfg, open(path, "w"), indent=2)
PY
}

verdicts() {   # verdicts <foton-id-or-hash> - the material verdicts ask reports, one per line
  cockpit ask "{\"query\":\"producer\",\"ref\":\"$1\"}" \
    | python3 -c 'import json,sys
for r in json.load(sys.stdin).get("records", []):
    for m in r.get("material", []): print(m["scheme"], m["verdict"], sep="\t")'
}

echo ""
echo "== 1. attached, checked, and reported as verified =="
configure verified
printf 'id,value\n1,42\n' > data/in.csv
cp data/in.csv work/out.csv
OUT=$(cockpit publish '{"cmd":"cp data/in.csv work/out.csv","inputs":["data/in.csv"],"outputs":["work/out.csv"]}')
HASH=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["work/out.csv"])' <<<"$OUT")
V=$(verdicts "$HASH")
require "the certificate is reported verified" grep -qx "x509-cert	verified" <<<"$V"

echo ""
echo "== 2. the unknown scheme rode along anyway, as CARRIED =="
# The rule the kernel states and the cockpit inherits. Refusing it would be this repo deciding which
# evidence is allowed to exist.
require "the unlisted scheme was attached, not rejected" \
  grep -qx "acme-internal-badge-v3	carried" <<<"$V"
absent "and it is not counted as verified" "acme-internal-badge-v3	verified" "$V"

echo ""
echo "== 3. a valid certificate for SOMEBODY ELSE's key is reported failed =="
# The negative control that matters: if any valid certificate were accepted, a record could borrow
# any identity, and "verified, not declared" would be back to declared.
configure wrong-key
printf 'id,value\n2,43\n' > data/in2.csv
cp data/in2.csv work/out2.csv
OUT2=$(cockpit publish '{"cmd":"cp data/in2.csv work/out2.csv","inputs":["data/in2.csv"],"outputs":["work/out2.csv"]}')
HASH2=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["work/out2.csv"])' <<<"$OUT2")
V2=$(verdicts "$HASH2")
require "a certificate belonging to another key is reported failed" grep -qx "x509-cert	failed" <<<"$V2"

echo ""
echo "== 4. no configured root: carried, never quietly verified =="
# The failure this guards against is silence. With no trust anchor there is nothing to judge
# against, and the honest answer is that nothing was judged — not a pass, and not a refusal to
# carry. Falling back to the host's root store would make the answer depend on the machine.
configure no-roots
printf 'id,value\n3,44\n' > data/in3.csv
cp data/in3.csv work/out3.csv
OUT3=$(cockpit publish '{"cmd":"cp data/in3.csv work/out3.csv","inputs":["data/in3.csv"],"outputs":["work/out3.csv"]}')
HASH3=$(python3 -c 'import json,sys; print(json.load(sys.stdin)["outputHashes"]["work/out3.csv"])' <<<"$OUT3")
V3=$(verdicts "$HASH3")
require "the same certificate is carried, not verified, with no root configured" \
  grep -qx "x509-cert	carried" <<<"$V3"

echo ""
echo "  Three verdicts and no fourth: verified (checked here, holds), carried (travels with the"
echo "  record, nothing here evaluated it), failed (checked here, does not hold). Collapsing"
echo "  carried into either direction is the failure — counting it as verified overstates, and"
echo "  omitting it means a reader cannot tell evidence-nobody-read from no evidence at all."

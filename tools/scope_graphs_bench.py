#!/usr/bin/env python3
"""Measure one-claims-graph (layout A) vs one-graph-per-scope (layout B) on an Oxigraph server.

Standard library only; run with `python3 -I`.

Input: the cockpit's claims graph (urn:kton:view:<slot>:claims) and records graph (urn:kton:view:<slot>)
exported as N-Triples, e.g.
  curl -G -H 'Accept: application/n-triples' --data-urlencode graph=urn:kton:view:<slot>:claims  $URL/store > claims.nt
  curl -G -H 'Accept: application/n-triples' --data-urlencode graph=urn:kton:view:<slot>         $URL/store > records.nt

Against a SCRATCH Oxigraph (it runs DROP ALL before loading!):
  python3 -I scope_graphs_bench.py --url http://127.0.0.1:7979 --claims claims.nt --records records.nt \
      --scales 0,11000,100000 --runs 5
  (scale 0 = the claims as exported; N = replicate claims synthetically to about N, spread over the
   real scopes plus --extra-scopes synthetic ones)
  --layouts A or --layouts B loads only that layout (for on-disk size comparison in separate stores);
  --load-only skips the queries.
  --query FILE (repeatable) also times a query of your own in A (default = records + claims graph) and in B with
  every scope graph listed as the default graph (the union per request).
  --query-graph FILE (repeatable) times a query rewritten for B that names scope graphs as {G:<scope label>}
  (e.g. GRAPH {G:dryrun1.mapping} { ... }); the placeholder becomes that scope's graph, found by the scope's
  rdfs:label in the export. It runs with default = records and named = every scope graph.
  Fill parameters ($VAR) into the files yourself; they are sent as they are. Use --scales 0 for real data.

Layout A: records -> urn:test:records, all claims -> urn:test:A:claims.
Layout B: records -> urn:test:records (same graph), claim-subject triples -> urn:test:scope:<hex of nk:scope>;
          triples about non-claim subjects (scope labels, agents) go to urn:test:B:other in B and to
          urn:test:A:claims in A (as in the cockpit today).
"""
import argparse, hashlib, json, re, statistics, sys, time, urllib.parse, urllib.request

NK = "https://kton.dev/v/"
RDF_TYPE = "<http://www.w3.org/1999/02/22-rdf-syntax-ns#type>"
CLAIM = "<" + NK + "Claim>"
P_SCOPE = "<" + NK + "scope>"
P_POS = "<" + NK + "position>"
P_ABOUT = "<" + NK + "about>"
G_REC = "urn:test:records"
G_A = "urn:test:A:claims"
G_BO = "urn:test:B:other"
PREFIX = ("PREFIX nk: <https://kton.dev/v/> PREFIX prov: <http://www.w3.org/ns/prov#> "
          "PREFIX xsd: <http://www.w3.org/2001/XMLSchema#> ")
POS_RE = re.compile(r'^"(-?\d+)"(\^\^<[^>]+>)?$')


def parse_nt(path):
    out = []
    with open(path, encoding="utf-8") as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            assert line.endswith(" ."), line
            s, p, o = line[:-2].split(" ", 2)
            out.append((s, p, o.strip()))
    return out


def scope_graph(scope_term):
    iri = scope_term.strip("<>")
    tail = iri.rsplit("/", 1)[-1]
    if not re.fullmatch(r"[0-9a-f]{64}", tail):
        tail = hashlib.sha256(iri.encode()).hexdigest()
    return "urn:test:scope:" + tail


def replicate(claims, target, extra_scopes):
    """Copies of the claim subjects with new IRIs until about `target` claims. Claim IRIs in object
    position are renamed with the same copy suffix; nk:scope is moved round-robin over the real scopes
    plus `extra_scopes` synthetic ones; nk:position is shifted per copy; nk:about to non-claims is kept."""
    subj = {s for s, p, o in claims if p == RDF_TYPE and o == CLAIM}
    real_scopes = sorted({o for s, p, o in claims if s in subj and p == P_SCOPE})
    scopes = real_scopes + ["<https://kton.dev/o/%064x>" % (0xabc000 + i) for i in range(extra_scopes)]
    idx = {s: i for i, s in enumerate(scopes)}
    ctriples = [t for t in claims if t[0] in subj]
    other = [t for t in claims if t[0] not in subj]
    n = len(subj)
    copies = max(1, -(-target // n))
    out = list(claims)
    for k in range(1, copies):
        suf = "/r%d" % k
        for s, p, o in ctriples:
            s2 = s[:-1] + suf + ">"
            if p == P_SCOPE:
                o2 = scopes[(idx[o] + k) % len(scopes)]
            elif p == P_POS:
                m = POS_RE.match(o)
                o2 = '"%d"%s' % (int(m.group(1)) + k * 1000, m.group(2) or "") if m else o
            elif o in subj:
                o2 = o[:-1] + suf + ">"
            else:
                o2 = o
            out.append((s2, p, o2))
    return out, len(subj) * copies


class Ox:
    def __init__(self, url, timeout):
        self.url = url.rstrip("/")
        self.timeout = timeout

    def _req(self, path, data, ctype, accept=None):
        r = urllib.request.Request(self.url + path, data=data, method="POST")
        r.add_header("Content-Type", ctype)
        if accept:
            r.add_header("Accept", accept)
        with urllib.request.urlopen(r, timeout=self.timeout) as resp:
            return resp.read()

    def update(self, q):
        self._req("/update", urllib.parse.urlencode({"update": q}).encode(), "application/x-www-form-urlencoded")

    def load_nquads(self, body):
        self._req("/store", body, "application/n-quads")

    def query(self, q, default=(), named=()):
        form = [("query", PREFIX + q)]
        form += [("default-graph-uri", g) for g in default]
        form += [("named-graph-uri", g) for g in named]
        return json.loads(self._req("/query", urllib.parse.urlencode(form).encode(),
                                    "application/x-www-form-urlencoded", "application/sparql-results+json"))


class nquads:
    """N-Quads of triples, posted in chunks of 100,000 lines to bound memory."""
    def __init__(self, triples, graph_of, chunk=100000):
        self.t, self.g, self.chunk = triples, graph_of, chunk


def _load(ox, nq):
    t = nq.t
    for i in range(0, len(t), nq.chunk):
        ox.load_nquads("".join("%s %s %s <%s> .\n" % (s, p, o, nq.g(s, p, o)) for s, p, o in t[i:i + nq.chunk]).encode())


def fingerprint(res):
    rows = sorted(json.dumps(b, sort_keys=True) for b in res["results"]["bindings"])
    return len(rows), hashlib.sha256("\n".join(rows).encode()).hexdigest()[:10]


def timed(ox, q, default, named, runs):
    res = ox.query(q, default, named)  # warm-up, not counted
    ts = []
    for _ in range(runs):
        t0 = time.perf_counter()
        res = ox.query(q, default, named)
        ts.append((time.perf_counter() - t0) * 1000)
    return statistics.median(ts), fingerprint(res)


def run_scale(ox, claims, records, scale, args):
    if scale:
        claims, nclaims = replicate(claims, scale, args.extra_scopes)
    subj = {s for s, p, o in claims if p == RDF_TYPE and o == CLAIM}
    scope_of = {s: o for s, p, o in claims if s in subj and p == P_SCOPE}
    sgraphs = sorted({scope_graph(o) for o in scope_of.values()})
    info = {"scale": scale or "real", "claims": len(subj), "scopes": len(sgraphs),
            "claim_triples": len(claims), "record_triples": len(records)}

    ox.update("DROP ALL")
    layouts = args.layouts
    if "A" in layouts:
        t0 = time.perf_counter()
        _load(ox, nquads(records, lambda *t: G_REC))
        _load(ox, nquads(claims, lambda *t: G_A))
        info["load_A_ms"] = round((time.perf_counter() - t0) * 1000)
    if "B" in layouts:
        t0 = time.perf_counter()
        if "A" not in layouts:
            _load(ox, nquads(records, lambda *t: G_REC))
        _load(ox, nquads(claims, lambda s, p, o: scope_graph(scope_of[s]) if s in subj else G_BO))
        info["load_B_ms"] = round((time.perf_counter() - t0) * 1000)
        if "A" in layouts:
            info["load_B_ms_note"] = "records already loaded by A"
    if args.load_only or layouts != "AB":
        return info, []

    # Scopes for Q2/Q4 (largest) and Q3 (pair sharing most nk:about subjects).
    by_scope = {}
    for s, o in scope_of.items():
        by_scope.setdefault(o, set()).add(s)
    X = max(by_scope, key=lambda k: (len(by_scope[k]), k))
    about = {}
    for s, p, o in claims:
        if p == P_ABOUT and s in subj:
            about.setdefault(scope_of[s], set()).add(o)
    pairs = [(len(about[a] & about[b]), a, b) for a in about for b in about if a < b]
    _, Q3x, Q3y = max(pairs) if pairs else (0, X, X)
    gX, gx, gy = scope_graph(X), scope_graph(Q3x), scope_graph(Q3y)
    info.update(q2_scope=X, q2_claims=len(by_scope[X]), q3_scopes=[Q3x, Q3y])

    defA = [G_REC, G_A]
    defB = [G_REC, G_BO] + sgraphs
    q1 = "SELECT ?p (COUNT(?c) AS ?n) WHERE { ?c a nk:Claim ; nk:predicate ?p } GROUP BY ?p"
    q2a = "SELECT ?p (COUNT(?c) AS ?n) WHERE { ?c a nk:Claim ; nk:scope %s ; nk:predicate ?p } GROUP BY ?p" % X
    q2b2 = "SELECT ?p (COUNT(?c) AS ?n) WHERE { GRAPH <%s> { ?c a nk:Claim ; nk:predicate ?p } } GROUP BY ?p" % gX
    q3a = ("SELECT (COUNT(*) AS ?n) WHERE { ?cx nk:scope %s ; nk:about ?s . ?cy nk:scope %s ; nk:about ?s . "
           "?out prov:wasGeneratedBy ?s }" % (Q3x, Q3y))
    q3b = ("SELECT (COUNT(*) AS ?n) WHERE { GRAPH <%s> { ?cx nk:about ?s } GRAPH <%s> { ?cy nk:about ?s } "
           "?out prov:wasGeneratedBy ?s }" % (gx, gy))
    q4a = ("SELECT ?s ?c WHERE { ?c nk:scope %s ; nk:about ?s ; nk:position ?p "
           "FILTER NOT EXISTS { ?c2 nk:scope %s ; nk:about ?s ; nk:position ?p2 FILTER(?p2 > ?p) } }" % (X, X))
    q4b = ("SELECT ?s ?c WHERE { GRAPH <%s> { ?c nk:about ?s ; nk:position ?p "
           "FILTER NOT EXISTS { ?c2 nk:about ?s ; nk:position ?p2 FILTER(?p2 > ?p) } } }" % gX)
    plan = [
        ("Q1 count/predicate all", "A", q1, defA, []),
        ("Q1 count/predicate all", "B default=records+all scopes", q1, defB, []),
        ("Q2 one scope", "A nk:scope pattern", q2a, defA, []),
        ("Q2 one scope", "B1 default=records+1 scope", q1, [G_REC, gX], []),
        ("Q2 one scope", "B2 GRAPH, named=all scopes", q2b2, defB, sgraphs),
        ("Q3 cross-scope join+records", "A", q3a, defA, []),
        ("Q3 cross-scope join+records", "B GRAPH, named=all scopes", q3b, defB, sgraphs),
        ("Q3 cross-scope join+records", "B GRAPH, default=records, named=X,Y", q3b, [G_REC], [gx, gy]),
        ("Q4 latest/subject in scope", "A", q4a, defA, []),
        ("Q4 latest/subject in scope", "B2 GRAPH, named=all scopes", q4b, defB, sgraphs),
    ]
    labels = {}
    scopes_set = set(scope_of.values())
    for s_, p_, o_ in claims + records:  # a scope's label is in either export
        if p_ == "<http://www.w3.org/2000/01/rdf-schema#label>" and s_ in scopes_set:
            labels[o_.split('"')[1] if o_.startswith('"') else o_] = scope_graph(s_)
    for f in args.query:
        q = open(f, encoding="utf-8").read()
        plan.append((f, "A (today)", q, defA, []))
        plan.append((f, "B default=records+all scopes", q, defB, []))
    for f in args.query_graph:
        q = open(f, encoding="utf-8").read()
        for name in re.findall(r"\{G:([^}]+)\}", q):
            if name not in labels:
                sys.exit("%s: no scope labelled %r in the export (have: %s)" % (f, name, ", ".join(sorted(labels))))
            q = q.replace("{G:%s}" % name, "<%s>" % labels[name])
        plan.append((f, "B GRAPH, default=records, named=all scopes", q, [G_REC, G_BO], sgraphs))
    rows = []
    for name, lay, q, d, n in plan:
        ms, (nrows, fp) = timed(ox, q, d, n, args.runs)
        rows.append({"query": name, "layout": lay, "median_ms": round(ms, 1), "rows": nrows, "result": fp})
        print("  %-30s %-38s %9.1f ms  rows=%d %s" % (name, lay, ms, nrows, fp), file=sys.stderr)
    return info, rows


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--url", required=True, help="a SCRATCH Oxigraph server; everything in it is dropped")
    ap.add_argument("--claims", required=True)
    ap.add_argument("--records", required=True)
    ap.add_argument("--scales", default="0", help="comma list; 0 = as exported")
    ap.add_argument("--extra-scopes", type=int, default=20)
    ap.add_argument("--runs", type=int, default=5)
    ap.add_argument("--layouts", default="AB", choices=["AB", "A", "B"])
    ap.add_argument("--load-only", action="store_true")
    ap.add_argument("--timeout", type=float, default=600)
    ap.add_argument("--query", action="append", default=[], help="a query file of your own, timed in A and B")
    ap.add_argument("--query-graph", action="append", default=[], help="a query naming scope graphs as {G:<label>}, timed in B")
    args = ap.parse_args()
    ox = Ox(args.url, args.timeout)
    claims, records = parse_nt(args.claims), parse_nt(args.records)
    results = []
    for sc in [int(x) for x in args.scales.split(",")]:
        print("scale %s" % (sc or "real"), file=sys.stderr)
        info, rows = run_scale(ox, claims, records, sc, args)
        print("  " + json.dumps(info), file=sys.stderr)
        results.append({"info": info, "rows": rows})
    print(json.dumps(results, indent=1))


if __name__ == "__main__":
    main()

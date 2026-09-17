#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Build docs/index.html — the page GitHub Pages serves for this repository.

Two things on that page are read out of the repository rather than written here, so the page cannot
drift from the tool it describes:

  * the tour's commands and answers come from docs/_data/transcript.json, captured by running them
    (docs/capture.sh);
  * each example's commands and assertions are parsed out of its own run.sh.

Only the prose is authored. `TestDocs_TheHomepageIsWhatBuildPyProduces` fails if index.html is not
what this produces, so a change to an example reaches the page or the suite says so.
"""
import html, io, json, os, re, sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)

TRANSCRIPT = json.load(open(os.path.join(HERE, "_data", "transcript.json")))

# The repo the transcript was captured in lived in a temp directory. Shortened for width, and the
# page says so rather than quietly presenting a path nobody has.
LONGPATH = re.compile(r"/tmp/[^\s\]]*?/repo")
def shorten(s): return LONGPATH.sub("~/sensor-repo", s)

R, F, A = "require", "expect_fail", "absent"
MARK   = {R: "▪", F: "▫", A: "–"}
PREFIX = {R: "ok:", F: "refused, as it must be:", A: "ok:"}

SQ = re.compile(r"cockpit (publish|say|ask) '((?:[^']|\n)*?)'")
DQ = re.compile(r'cockpit (publish|say|ask) "((?:\\.|[^"\\])*)"')
AS = re.compile(r'^(require|expect_fail|absent) +"([^"]+)"', re.M)


def read_example(slug):
    """The commands an example issues and the assertions it makes, from its own script."""
    src = open(os.path.join(ROOT, "examples", slug, "run.sh")).read()
    found = []
    for verb, arg in SQ.findall(src):
        found.append((src.index(arg), verb, " ".join(arg.split())))
    for verb, arg in DQ.findall(src):
        found.append((src.index(arg), verb, " ".join(arg.replace('\\"', '"').split())))
    found.sort()
    calls = []
    for _, verb, arg in found:
        if (verb, arg) not in calls:
            calls.append((verb, arg))
    return calls, AS.findall(src)


TOUR = [
 ("Is this repository set up, and bound to what it says?",
  "The first thing anyone runs. It reads the configuration, checks the origin remote matches what "
  "the config claims, and names the kernel compiled into the binary. The working directory is "
  "shortened here for width."),
 ("Record a result.",
  "Three things in — the command, what it read, what it wrote — and everything else out: the "
  "foton's id, the hash of every output, the commit that holds them, and a permalink pinned to that "
  "commit. The caller did not commit, push, build a permalink, choose a key or sign. Those are the "
  "things it cannot get subtly wrong, because it never touches them."),
 ("Ask what produced a result.",
  "By the hash of the bytes, not by a filename. <b>verified: true</b> and <b>tier: self</b> are the "
  "answer's own work: every record was re-verified against this repository's configured keys before "
  "being included, and never trusted from the keyid it declares about itself."),
 ("Say something about it.",
  "From a template this repository allows, and nothing else — so what can be said is decided by "
  "files an operator put in <code>templates/</code>. The answer confirms registration by reading "
  "the claim back: an id coming back means it was signed, not that the store kept it."),
 ("Ask what is said about it.",
  "The claim comes back with its object — what was actually said — and the key that signed it, not "
  "merely the fact that a claim exists."),
 ("Get an argument wrong.",
  "<code>output</code> where the verb takes <code>outputs</code>. Over MCP a tool schema catches "
  "that; at a shell nothing else would, and the record would have been published naming no outputs "
  "at all — signed, valid, and wrong. <b>Exit 1 means the call itself was wrong.</b>"),
 ("Point it at the wrong repository.",
  "The guard this project exists for. A live demo once had six registries under one parent "
  "directory and a session cooperated against the wrong one. Nothing failed — the records were "
  "signed, valid, and in the wrong project. <b>Exit 2 means the cockpit understood, and "
  "declined.</b>"),
]

EX = [
 ("01-publish", "publish", [],
  "One call records a signed foton — and the point is everything the caller did <em>not</em> do."),
 ("02-chain", "chain", [],
  "Two steps join because they share a hash, not because anyone said they follow."),
 ("03-anti-wrong-folder", "anti-wrong-folder", [],
  "The guard, and the reason this project exists. A session once cooperated against the wrong "
  "registry and <em>nothing failed</em>."),
 ("04-claim-ceiling", "claim-ceiling", [],
  "What may be said, and what may not be decided. A claim comes from a fixed set of templates, and "
  "for <code>reproduces</code> the level is measured rather than stated."),
 ("05-trust-tiers", "trust-tiers", [],
  "Verified, not declared. A stranger's record goes into the same registry and is still excluded — "
  "then the key is configured into a tier and the same record is included. The record did not "
  "change. The configuration did."),
 ("06-reproduce", "reproduce", [],
  "Reproducing does not add a record. It adds a signature to the one that is already there."),
 ("07-run-in-container", "run-in-container", ["docker"],
  "The environment pin stops being a claim: the cockpit runs the command itself, so the string "
  "handed to the engine and the string recorded are one string."),
 ("08-union-and-show", "union-and-show", [],
  "The graph, published and served, without parsing a registry file. <code>keys.json</code> comes "
  "from the configured tiers, so a viewer re-verifies against exactly what a query does."),
 ("09-carried-evidence", "carried-evidence", ["openssl"],
  "A record says <em>which</em> key signed it, not whose key that is. Evidence about a record is "
  "attached whatever its scheme, and reported as verified, carried, or failed."),
 ("10-cleaning-is-an-argument", "cleaning-is-an-argument", ["R", "network"],
  "Two analyses of the same open data, one line apart, both honest, disagreeing by centuries. "
  "Nothing here is a lie detector — the script is an input, by hash, so “which did you run” is a "
  "lookup instead of a question."),
 ("11-environment-reconstructible", "environment-reconstructible", ["nix", "docker", "network"],
  "A pin identifies; a derivation reconstructs. The image is built from a Nix expression, the "
  "expression is an input to the record, and the digest follows from it."),
 ("12-the-environment-written-in-r", "the-environment-written-in-r", ["nix", "docker", "network"],
  "Nobody writes Nix. An R user who has to learn a second language to pin their environment will "
  "not pin it — so the environment is declared in R, and the digest follows from that file."),
]

BUILDS_ON = {"09-carried-evidence": "05", "11-environment-reconstructible": "07",
             "12-the-environment-written-in-r": "11"}


def esc(s):
    return html.escape(s, quote=False)


def terminal(cmd, out, code, limit=30):
    lines = shorten(out).split("\n")
    elided = max(0, len(lines) - limit)
    lines = lines[:limit]
    b = io.StringIO()
    b.write('<div class="term">')
    b.write(f'<div class="cmdline"><span class="dollar">$</span>{esc(shorten(cmd))}</div>')
    b.write('<pre class="res">' + esc("\n".join(lines)))
    if elided:
        b.write(f'\n<span class="elide">… {elided} more lines</span>')
    b.write("</pre>")
    if code:
        b.write(f'<div class="code c{code}">exit {code}</div>')
    b.write("</div>")
    return b.getvalue()


def main():
    examples = [(slug, name, needs, thesis) + read_example(slug) for slug, name, needs, thesis in EX]
    tot = sum(len(a) for *_, a in examples)
    nf = sum(1 for *_, a in examples for k, _ in a if k == F)
    noneed = sum(1 for _, _, needs, *_ in examples if not needs)

    o = io.StringIO()
    w = o.write
    w(HEAD)
    w(f'''<div class="wrap">
<header class="mast">
  <div class="eyebrow">kton-cockpit</div>
  <h1>Three verbs, and a record somebody else can <em>check</em>.</h1>
  <p class="lede">A command-line tool for doing work in a <a href="https://kton.dev">kton</a>
  federation and leaving evidence of it. You name what went in, what came out, and the command you
  ran. It signs, commits, pushes, pins the permalinks, and answers questions about the graph
  afterwards — <strong>re-verifying every record against keys you configured</strong>, never
  trusting one because it turned up in the registry.</p>
  <div class="verbs">
    <span class="verb"><b>cockpit publish</b> record a result</span>
    <span class="verb"><b>cockpit say</b> bind a claim</span>
    <span class="verb"><b>cockpit ask</b> query the graph</span>
  </div>
</header>

<section class="sec">
  <h2>A tour, end to end</h2>
  <p class="sub">Every command below was run against a participant repository built from nothing,
  and every answer is what came back. Nothing here is illustrative.</p>
''')
    for (head, note), step in zip(TOUR, TRANSCRIPT):
        w('  <div class="step">\n')
        w(f'    <div class="stephead"><h3>{head}</h3><p>{note}</p></div>\n')
        w("    " + terminal(step["cmd"], step["out"], step["code"]) + "\n")
        w("  </div>\n")

    w(f'''</section>

<section class="sec">
  <h2>Twelve examples, one property each</h2>
  <p class="sub">Each builds a complete participant repository from nothing, exercises one property,
  and prints what it saw. The lines under each card are its real assertions. A check that only
  looked for the absence of an error would pass when a query returned nothing, which is how a suite
  reports success having verified nothing — so these count what they found.</p>
  <div class="figs">
    <div class="fig"><b>{len(examples)}</b><span>examples</span></div>
    <div class="fig"><b>{tot}</b><span>assertions</span></div>
    <div class="fig"><b>{nf}</b><span>negative controls</span></div>
    <div class="fig"><b>{noneed}</b><span>need nothing installed</span></div>
  </div>
  <div class="legend">
    <span class="lg"><i class="ok">▪</i><b>ok:</b> something was true</span>
    <span class="lg"><i>▫</i><b>refused, as it must be:</b> a call that had to fail, did</span>
    <span class="lg"><i>–</i><b>ok:</b> something was absent, and that was the claim</span>
  </div>
  <div class="grid">
''')
    for slug, name, needs, thesis, calls, asserts in examples:
        w('    <article class="card">\n')
        w(f'      <div class="hd"><span class="num">{slug[:2]}</span><h3>{esc(name)}</h3></div>\n')
        w(f'      <p class="thesis">{thesis}</p>\n')
        w('      <div class="meta">')
        if needs:
            for n in needs:
                w(f'<span class="chip need">needs {esc(n)}</span>')
        else:
            w('<span class="chip free">no prerequisites</span>')
        if slug in BUILDS_ON:
            w(f'<span class="chip">builds on {BUILDS_ON[slug]}</span>')
        w("</div>\n")
        w('      <div class="calls">\n')
        for verb, arg in calls[:3]:
            w(f'        <div class="call"><span class="dollar">$</span>'
              f"cockpit {verb} '{esc(arg)}'</div>\n")
        w("      </div>\n      <ul class=\"out\">\n")
        for kind, text in asserts:
            cls = {R: "", F: ' class="f"', A: ' class="a"'}[kind]
            w(f'        <li{cls}><i>{MARK[kind]}</i>'
              f'<span><span class="p">{PREFIX[kind]}</span> '
              f'<span class="t">{esc(text)}</span></span></li>\n')
        w("      </ul>\n    </article>\n")

    w(FOOT)
    out = o.getvalue()
    path = os.path.join(HERE, "index.html")
    if "--check" in sys.argv:
        if open(path).read() != out:
            sys.exit("docs/index.html is not what build.py produces — re-run docs/build.py")
        print("docs/index.html is current")
        return
    open(path, "w").write(out)
    print(f"wrote {path} ({len(out)} bytes, {len(examples)} examples, {tot} assertions)")


HEAD = '''<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>cockpit — three verbs, and a record somebody else can check</title>
<meta name="description" content="A command-line tool for doing work in a kton federation and leaving evidence of it: publish, say, ask.">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Newsreader:ital,opsz,wght@0,6..72,400;0,6..72,500;0,6..72,600;1,6..72,400&family=IBM+Plex+Mono:wght@400;500;600&family=IBM+Plex+Sans:wght@400;500;600&display=swap">
<style>
:root{
  --paper:#edf0ee; --surface:#f7f9f8; --sunk:#e4e9e6;
  --ink:#10221f; --ink-2:#47605b; --ink-3:#6d837e;
  --rule:#d2d9d5; --rule-2:#c0cac5;
  --accent:#0e6b63; --accent-ink:#0a534d; --accent-soft:rgba(14,107,99,.10);
  --warn:#8a4a1c; --stop:#8c2f3d;
  --shadow:0 1px 2px rgba(16,34,31,.05);
  color-scheme:light dark;
}
@media (prefers-color-scheme:dark){:root{
  --paper:#0c1412; --surface:#121d1a; --sunk:#09100e;
  --ink:#dfe8e4; --ink-2:#8ba49d; --ink-3:#71887f;
  --rule:#22332e; --rule-2:#2d423b;
  --accent:#4fbfae; --accent-ink:#77d3c4; --accent-soft:rgba(79,191,174,.13);
  --warn:#d69a63; --stop:#e0808f;
  --shadow:0 1px 2px rgba(0,0,0,.3);
}}
*{box-sizing:border-box}
html{-webkit-text-size-adjust:100%}
body{margin:0;background:var(--paper);color:var(--ink);
  font-family:"IBM Plex Sans",system-ui,-apple-system,"Segoe UI",sans-serif;
  font-size:15px;line-height:1.6;padding-inline:clamp(16px,3vw,40px);
  -webkit-font-smoothing:antialiased}
.wrap{max-width:1500px;margin:0 auto}
code,.mono{font-family:"IBM Plex Mono",ui-monospace,"SF Mono",Menlo,monospace}
h1,h2,h3{font-family:Newsreader,Georgia,"Times New Roman",serif;font-weight:500;
  text-wrap:balance;margin:0}
a{color:var(--accent-ink)}
:focus-visible{outline:2px solid var(--accent);outline-offset:2px;border-radius:2px}

.eyebrow{font-family:"IBM Plex Mono",monospace;font-size:11px;font-weight:500;
  letter-spacing:.14em;text-transform:uppercase;color:var(--ink-3)}
.mast{padding-block:clamp(36px,6vw,64px) 34px}
.mast h1{font-size:clamp(34px,5vw,62px);line-height:1.04;letter-spacing:-.02em;
  margin-top:14px;max-width:20ch}
.mast h1 em{font-style:italic;color:var(--accent-ink)}
.lede{max-width:74ch;color:var(--ink-2);font-size:clamp(15.5px,1.2vw,17.5px);margin:18px 0 0}
.lede strong{color:var(--ink);font-weight:500}
.verbs{display:flex;flex-wrap:wrap;gap:8px;margin-top:24px}
.verb{font-family:"IBM Plex Mono",monospace;font-size:13px;border:1px solid var(--rule-2);
  border-radius:2px;padding:6px 12px;background:var(--surface);color:var(--ink-3)}
.verb b{color:var(--accent-ink);font-weight:600;margin-right:8px}

.sec{padding-block:clamp(28px,3.5vw,44px);border-top:1px solid var(--rule)}
.sec>h2{font-size:clamp(25px,2.8vw,36px);letter-spacing:-.014em}
.sec>.sub{color:var(--ink-2);max-width:74ch;margin:10px 0 0}

/* The terminal always gets the full content width: a command is one long line and a reader who
   has to scroll a 400px box to see it has been shown a screenshot of a tool, not the tool. */
.step{padding-block:clamp(20px,2.4vw,30px);border-bottom:1px solid var(--rule)}
.step:last-child{border-bottom:0}
.stephead{max-width:78ch;margin-bottom:14px}
.step h3{font-size:clamp(18px,1.6vw,22px)}
.step p{margin:6px 0 0;color:var(--ink-2);font-size:14.5px}
.step p code{font-size:.92em;background:var(--accent-soft);padding:1px 4px;border-radius:2px}
.step p b{color:var(--ink);font-weight:500}

.term{background:var(--sunk);border:1px solid var(--rule);border-radius:3px;overflow:hidden}
.cmdline{font-family:"IBM Plex Mono",monospace;font-size:clamp(12px,1vw,13.5px);line-height:1.6;
  padding:12px 14px;border-bottom:1px solid var(--rule);color:var(--ink);
  background:var(--surface);white-space:pre-wrap;overflow-wrap:anywhere}
.dollar{color:var(--accent);user-select:none;margin-right:8px}
.res{font-family:"IBM Plex Mono",monospace;font-size:clamp(11.5px,.92vw,13px);line-height:1.6;
  margin:0;padding:12px 14px;overflow-x:auto;color:var(--ink-2);white-space:pre}
.elide{color:var(--ink-3);font-style:italic}
.code{font-family:"IBM Plex Mono",monospace;font-size:10.5px;letter-spacing:.08em;
  text-transform:uppercase;padding:6px 14px;border-top:1px solid var(--rule);font-weight:500}
.code.c1{color:var(--warn)}
.code.c2{color:var(--stop)}

.figs{display:flex;flex-wrap:wrap;gap:12px 48px;margin-top:28px;font-variant-numeric:tabular-nums}
.fig{display:flex;flex-direction:column}
.fig b{font-family:Newsreader,Georgia,serif;font-size:clamp(26px,2.4vw,34px);font-weight:600;
  line-height:1.1}
.fig span{font-family:"IBM Plex Mono",monospace;font-size:11px;letter-spacing:.09em;
  text-transform:uppercase;color:var(--ink-3)}

.legend{display:flex;flex-wrap:wrap;gap:8px 30px;margin-top:22px}
.lg{font-family:"IBM Plex Mono",monospace;font-size:12px;color:var(--ink-2)}
.lg i{font-style:normal;color:var(--ink-3);margin-right:7px}
.lg i.ok{color:var(--accent)}
.lg b{color:var(--ink);font-weight:500;margin-right:5px}

.grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(400px,1fr));gap:22px;
  align-items:start;margin-top:32px}
.card{background:var(--surface);border:1px solid var(--rule);border-radius:3px;
  box-shadow:var(--shadow);padding:22px 22px 8px;display:flex;flex-direction:column;gap:13px}
.hd{display:flex;align-items:baseline;gap:11px}
.num{font-family:"IBM Plex Mono",monospace;font-size:12px;font-weight:600;color:var(--accent);
  letter-spacing:.06em}
.card h3{font-size:21px;line-height:1.2;letter-spacing:-.008em}
.thesis{margin:0;font-size:14px;color:var(--ink-2)}
.thesis code{font-size:.92em;background:var(--accent-soft);padding:1px 4px;border-radius:2px}
.meta{display:flex;flex-wrap:wrap;gap:6px}
.chip{font-family:"IBM Plex Mono",monospace;font-size:10.5px;font-weight:500;letter-spacing:.07em;
  text-transform:uppercase;border:1px solid var(--rule-2);color:var(--ink-2);
  padding:3px 7px;border-radius:2px}
.chip.need{border-color:var(--accent);color:var(--accent-ink);background:var(--accent-soft)}
.chip.free{border-style:dashed}
.calls{display:flex;flex-direction:column;gap:5px}
.call{font-family:"IBM Plex Mono",monospace;font-size:11.5px;line-height:1.55;
  background:var(--sunk);border:1px solid var(--rule);border-radius:2px;padding:7px 10px;
  color:var(--ink-2);white-space:pre-wrap;overflow-wrap:anywhere}
.call .dollar{margin-right:6px}
.out{list-style:none;margin:0;padding:13px 0 15px;border-top:1px solid var(--rule);
  display:flex;flex-direction:column;gap:5px}
.out li{display:grid;grid-template-columns:1.1em 1fr;gap:5px;align-items:baseline;
  font-family:"IBM Plex Mono",monospace;font-size:12px;line-height:1.5;color:var(--ink-2)}
.out i{font-style:normal;color:var(--accent);font-size:10.5px}
.out li.f i,.out li.a i{color:var(--ink-3)}
.out .t{color:var(--ink)}
.out .p{color:var(--ink-3)}

.foot{border-top:1px solid var(--rule);padding-block:34px 56px;display:grid;
  grid-template-columns:repeat(auto-fit,minmax(280px,1fr));gap:28px 48px}
.foot h3{font-size:18px;margin-bottom:7px}
.foot p{margin:0;color:var(--ink-2);font-size:13.5px}
.foot p+p{margin-top:8px}
.foot pre{font-family:"IBM Plex Mono",monospace;font-size:12.5px;background:var(--sunk);
  border:1px solid var(--rule);border-radius:2px;padding:11px 13px;overflow-x:auto;
  margin:11px 0 0;color:var(--ink)}
@media (max-width:520px){.grid{grid-template-columns:1fr}}
</style>
</head>
<body>
'''

FOOT = '''  </div>
</section>

<footer class="foot">
  <div>
    <h3>Run the examples</h3>
    <p>Each builds its own participant repository — a real git repo with a real github.com origin,
    so the wrong-folder guard runs unmodified. Only the remote's <em>push</em> url points at a local
    bare repo, so everything completes offline.</p>
    <pre>examples/run-all.sh
examples/02-chain/run.sh</pre>
  </div>
  <div>
    <h3>Two exit codes</h3>
    <p><b>2</b> means the cockpit understood and refused. <b>1</b> means the call itself was wrong.
    A script needs to tell those apart, so they differ.</p>
    <p>Unknown fields are refused rather than ignored, because at a shell nothing else catches a
    typo in an argument name.</p>
  </div>
  <div>
    <h3>An agent can call them too</h3>
    <p>The same three verbs are an MCP tool surface, as <code class="mono">cockpit_publish</code>,
    <code class="mono">cockpit_say</code> and <code class="mono">cockpit_ask</code>. That is a
    second transport onto the same three handlers, not a second tool — a guard belongs to the
    configuration, never to how the call arrived.</p>
  </div>
  <div>
    <h3>This page</h3>
    <p>Built by <code class="mono">docs/build.py</code> from the example scripts and a transcript
    captured by <code class="mono">docs/capture.sh</code>. A test fails if it is out of date, so an
    example that changes reaches the page or the suite says so.</p>
  </div>
</footer>
</div>
</body>
</html>
'''

if __name__ == "__main__":
    main()

"""Run an analyst's steps in their repository and publish each with the cockpit: the step itself
(cockpit publish), its description and rationale, and the claims of its model development
(parent-step, based-on, key-step, base-model, final-model, qc-passed, reproduces), all signed by
the analyst and scoped. Fotons of every analyst are kept in a shared map, so one analyst can
name another's step ("carla:03").

    publish_analyst.py <analysts dir>/<name>/steps.json <repo dir> <scope name> <fotons.json>
"""
import hashlib, json, os, shutil, subprocess, sys

steps_file, repo, scope, map_file = sys.argv[1:5]
here = os.path.dirname(os.path.abspath(steps_file))
spec = json.load(open(steps_file))
name = os.path.basename(here)
fotons = json.load(open(map_file)) if os.path.exists(map_file) else {}


def cockpit(*args):
    r = subprocess.run(["cockpit", *args], cwd=repo, capture_output=True, text=True)
    if r.returncode != 0:
        sys.exit(f"cockpit {args[0]} in {repo}: {r.stderr.strip() or r.stdout.strip()}")
    return r.stdout


def say(template, subject, fields, **extra):
    cockpit("say", json.dumps({"template": template, "subject": subject, "fields": fields, "scope": scope, **extra}))


def ref(key):
    return fotons[key if ":" in key else f"{name}:{key}"]


def sha(path):
    return "sha256:" + hashlib.sha256(open(path, "rb").read()).hexdigest()


for src, dst in spec.get("copy", []):
    os.makedirs(os.path.join(repo, os.path.dirname(dst)), exist_ok=True)
    shutil.copyfile(os.path.join(here, src), os.path.join(repo, dst))

for st in spec["steps"]:
    for src, dst in st.get("copy_repo", []):
        os.makedirs(os.path.join(repo, os.path.dirname(dst)), exist_ok=True)
        shutil.copyfile(os.path.join(repo, src), os.path.join(repo, dst))
    for out in st["out"]:
        os.makedirs(os.path.join(repo, os.path.dirname(out)), exist_ok=True)
    r = subprocess.run(st["cmd"], shell=True, cwd=repo, capture_output=True, text=True)
    if r.returncode != 0:
        sys.exit(f"{name} {st['id']}: {st['cmd']} failed:\n{r.stderr[-2000:]}")
    out = cockpit("publish", json.dumps({"inputs": st["in"], "outputs": st["out"], "cmd": st["cmd"]}))
    foton = json.loads(out)["fotonId"]
    fotons[f"{name}:{st['id']}"] = foton
    say("describes", foton, {"value": st["describes"]})
    say("rationale", foton, {"value": st["rationale"]})
    for c in st.get("claims", []):
        subject = ref(c["about"]) if "about" in c else foton
        t = c["template"]
        if t in ("parent-step", "based-on"):
            target = ref(c["ref_step"]) if "ref_step" in c else sha(os.path.join(here, c["file"]))
            fields = {t: target}
        elif t == "reproduces":
            # the cockpit checks the reproduction itself (the bytes, the level) before it signs
            mine = st["out"][0]
            say(t, subject, {}, subjectOutputHash=sha(os.path.join(repo, mine)), reproducedOutput=mine, reproducedFotonId=foton)
            continue
        else:
            fields = {"value": c["value"]}
        if c.get("why"):
            fields["why"] = c["why"]
        say(t, subject, fields)
    print(f"  {name:7} {st['id']:7} {foton[:19]}  {len(st.get('claims', []))} claim(s)")

json.dump(fotons, open(map_file, "w"), indent=1)

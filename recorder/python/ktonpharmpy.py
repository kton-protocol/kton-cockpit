"""ktonpharmpy: what a pharmpy session derived from what, for the kton recorder (MH 21.11.).

pharmpy's modeling functions take a model and give a new one: add_lag_time(run1) is run1 with a
lag time. The shim sees files only, so a model developed in the console, read from run1.mod and
written as run2.mod, looks like two unrelated files. This wraps pharmpy.modeling's functions and
writes one DERIVE line per call that takes or gives a model, into the same log as ktonrec's TASK
lines:

  DERIVE  -  <path or ->  {"fn": "add_lag_time", "in": ["<model hash>"], "out": "<model hash>"}

read_model names the file a model came from (path, out), write_model the file it went to (path,
in). ktonwatch follows a written model back through the calls to the files it was read from, and
the story proposes the written file's step as a child step (pmx/parent-step) of the step that made
the file it was developed from. Only the outermost call is written (pharmpy's functions call each
other). A model's hash is over its name, its code and its dataset, so the same model read twice
has the same hash. Recording never changes what a function returns or raises.
"""
import functools
import hashlib
import json
import os

_state = {"depth": 0, "on": False, "line": None}


def model_hash(m):
    """sha256 over the model's name, code and dataset; None for what is not a model."""
    code = getattr(m, "code", None)
    if not isinstance(code, str):
        return None
    h = hashlib.sha256()
    h.update(str(getattr(m, "name", "")).encode("utf-8", "replace"))
    h.update(b"\0")
    h.update(code.encode("utf-8", "replace"))
    h.update(b"\0")
    ds = getattr(m, "dataset", None)
    if ds is not None:
        try:
            import pandas as pd  # pharmpy's own dependency

            h.update(pd.util.hash_pandas_object(ds, index=True).values.tobytes())
        except Exception:  # an unusual dataset: its text instead
            h.update(repr(ds).encode("utf-8", "replace"))
    return h.hexdigest()


def _models_in(args, kwargs):
    out = []
    for v in list(args) + list(kwargs.values()):
        mh = model_hash(v)
        if mh:
            out.append(mh)
    return out


def _path_arg(args, kwargs, names, pos):
    for n in names:
        if n in kwargs:
            return kwargs[n]
    return args[pos] if len(args) > pos else None


def _wrap(name, f):
    @functools.wraps(f)
    def call(*args, **kwargs):
        if _state["depth"] > 0:  # pharmpy calling itself: only the person's call is written
            return f(*args, **kwargs)
        _state["depth"] += 1
        try:
            result = f(*args, **kwargs)
        finally:
            _state["depth"] -= 1
        try:
            _derive(name, args, kwargs, result)
        except Exception:  # recording must never disturb the work
            pass
        return result

    call.__kton__ = True
    return call


def _derive(name, args, kwargs, result):
    ins = _models_in(args, kwargs)
    out = model_hash(result)
    path = None
    if name == "read_model":
        p = _path_arg(args, kwargs, ("path",), 0)
        path = os.path.abspath(os.fspath(p)) if p is not None else None
    elif name == "write_model":
        p = _path_arg(args, kwargs, ("path",), 1)
        path = os.path.abspath(os.fspath(p)) if p not in (None, "") else None
        if path is None and ins:  # pharmpy's default: <name>.<ext> in the working directory
            m = args[0] if args else kwargs.get("model")
            path = os.path.abspath(str(getattr(m, "name", ""))) if m is not None else None
        out = None
    if not ins and not out:
        return  # neither takes nor gives a model: not a derivation
    if out and out in ins and name not in ("read_model",):
        return  # gave back the model it was given (a query, not a change)
    rec = {"fn": name, "in": ins}
    if out:
        rec["out"] = out
        rec["name"] = str(getattr(result, "name", ""))
    _state["line"]("DERIVE", "-", path or "-", json.dumps(rec, separators=(",", ":")))


def wrap_modeling(modeling, line):
    """Wrap every public function of pharmpy.modeling (once); line writes a log line."""
    if getattr(modeling, "__kton__", False):
        return False
    _state["line"] = line
    for name in dir(modeling):
        if name.startswith("_"):
            continue
        f = getattr(modeling, name)
        if callable(f) and not isinstance(f, type) and not getattr(f, "__kton__", False):
            setattr(modeling, name, _wrap(name, f))
    modeling.__kton__ = True
    _state["on"] = True
    return True

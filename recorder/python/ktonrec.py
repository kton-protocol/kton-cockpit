"""ktonrec for Python: command boundaries for the kton recorder in an IPython kernel
(Positron's Python console, Jupyter).

The shim (libktontrace.so) sees every file a process touches; it does not know where one
console command ends and the next begins. As r/ktonrec does for R, this writes those
boundaries into the same log:

  ENV   once: the interpreter's own directories, so the watcher counts them as environment,
        the working directory as the project, and lang=python
  TASK  after every execution (IPython post_run_cell), status ok|err, with the code
  END   when the kernel exits

Loaded by the IPython startup file recorder/install.sh places (50-kton.py), and only when the
process runs under the shim. Which file the code came from comes from the Positron extension.
"""
import atexit
import os
import site
import sys
import time

_state = {}


def _esc(s):
    # the shim's v2 escaping, so a line stays a line and the path stays the path
    return s.replace("\\", "\\\\").replace("\t", "\\t").replace("\n", "\\n").replace("\r", "\\r")


def _paused():
    return os.path.exists(os.path.join(os.path.dirname(_state["log"]), "PAUSED"))


def _line(op, mode="-", path="-", extra="-"):
    if op not in ("ENV", "END") and _paused():
        return
    line = "%d\t%d\t0\t%s\t%s\t%s\t0\t0\t0\t%s\n" % (
        time.time_ns(), os.getpid(), op, mode, _esc(path) or "-", _esc(extra) or "-")
    fd = os.open(_state["log"], os.O_WRONLY | os.O_APPEND)  # one write per line, as the shim
    try:
        os.write(fd, line.encode("utf-8", "replace"))
    finally:
        os.close(fd)


def _env_dirs():
    dirs = {sys.prefix, sys.base_prefix, sys.exec_prefix, os.path.dirname(os.path.abspath(__file__))}
    try:
        dirs.update(site.getsitepackages())
        dirs.add(site.getusersitepackages())
    except AttributeError:  # a virtualenv's old site module
        pass
    for mod in ("IPython", "ipykernel", "positron"):  # the kernel's own files
        m = sys.modules.get(mod)
        if m is not None and getattr(m, "__file__", None):
            dirs.add(os.path.dirname(os.path.dirname(m.__file__)))
    return sorted(d for d in dirs if d)


def _post_run_cell(result):
    _state["n"] += 1
    info = getattr(result, "info", None)
    code = getattr(info, "raw_cell", "") or ""
    _line("TASK", "ok" if getattr(result, "success", True) else "err", str(_state["n"]), code[:300])


def start(ip):
    """Start writing boundaries for the IPython shell ip. Does nothing without a log."""
    log = os.environ.get("KTON_TRACE_LOG", "")
    if not log or "%p" in log or ip is None or _state.get("on"):
        return False
    _state.update(log=log, n=0, on=True)
    _line("ENV", "-", ",".join(_env_dirs()), "project=%s;lang=python" % os.getcwd())
    ip.events.register("post_run_cell", _post_run_cell)
    atexit.register(lambda: _line("END"))
    return True

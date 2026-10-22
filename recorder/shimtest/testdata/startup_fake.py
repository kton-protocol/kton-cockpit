"""Runs the IPython startup file 50-kton.py as IPython does (exec with get_ipython), after
install.sh's substitution of the path, then a cell; then one cell with the log moved away.
argv: startup file, path to put in, "expect-start" or "expect-nothing"."""
import os, sys

src = open(sys.argv[1], encoding="utf-8").read().replace('"@KTON_PY@"', repr(sys.argv[2]))  # as install.sh


class Shell:
    cbs = []
    events = None

    def register(self, name, cb):
        self.cbs.append(cb)


ip = Shell()
ip.events = ip
exec(compile(src, "50-kton.py", "exec"), {"get_ipython": lambda: ip, "__name__": "__main__"})
started = bool(ip.cbs)
assert started == (sys.argv[3] == "expect-start"), (started, sys.argv[3])
if started:
    info = type("Info", (), {"raw_cell": "x = 1"})()
    res = type("Result", (), {"info": info, "success": True})()
    for cb in ip.cbs:
        cb(res)
    log = os.environ["KTON_TRACE_LOG"]
    os.rename(log, log + ".away")  # the log is gone for a moment: no error, nothing printed
    for cb in ip.cbs:
        cb(res)
    os.rename(log + ".away", log)

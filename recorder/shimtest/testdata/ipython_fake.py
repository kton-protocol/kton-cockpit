"""Stands in for an IPython kernel: loads ktonrec as the startup file does and runs two
"cells" through post_run_cell, reading and writing files in between."""
import os, sys
sys.path.insert(0, sys.argv[1])
import ktonrec


class Shell:
    def __init__(self):
        self.cbs = []
        self.events = self

    def register(self, name, cb):
        assert name == "post_run_cell"
        self.cbs.append(cb)

    def cell(self, code, body, ok=True):
        body()
        info = type("Info", (), {"raw_cell": code})()
        for cb in self.cbs:
            cb(type("Result", (), {"info": info, "success": ok})())


ip = Shell()
assert ktonrec.start(ip)
ip.cell('%run -- "step1.py"', lambda: open("mid.csv", "w").write(open("in.csv").read() + "1\n"))
ip.cell("x = open('mid.csv').read()\nopen('out.csv', 'w').write(x)", lambda: open("out.csv", "w").write(open("mid.csv").read()))

# kton recorder: command boundaries for this IPython kernel (Positron's Python console), only
# when it runs under the recorder's shim. Placed by recorder/install.sh, which fills in
# _KTON_PY as a Python string literal.
import os as _os
import sys as _sys

_KTON_PY = "@KTON_PY@"

if _os.environ.get("KTON_TRACE_LOG") and "libktontrace" in _os.environ.get("LD_PRELOAD", ""):
    _sys.path.insert(0, _KTON_PY)
    _bytecode = _sys.dont_write_bytecode
    _sys.dont_write_bytecode = True  # no ktonrec .pyc as a write of the console's start
    try:
        import ktonrec as _ktonrec
        _ktonrec.start(get_ipython())  # noqa: F821 (IPython provides it to startup files)
    except Exception as _e:  # recording must never break the console
        print("kton: no command boundaries:", _e, file=_sys.stderr)
    finally:
        _sys.dont_write_bytecode = _bytecode
        _sys.path.remove(_KTON_PY)

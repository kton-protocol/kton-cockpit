# kton recorder: command boundaries for this IPython kernel (Positron's Python console), only
# when it runs under the recorder's shim. Placed by recorder/install.sh; @KTON_PY@ is filled in.
import os as _os
import sys as _sys

if _os.environ.get("KTON_TRACE_LOG") and "libktontrace" in _os.environ.get("LD_PRELOAD", ""):
    _sys.path.insert(0, "@KTON_PY@")
    try:
        import ktonrec as _ktonrec
        _ktonrec.start(get_ipython())  # noqa: F821 (IPython provides it to startup files)
    except Exception as _e:  # recording must never break the console
        print("kton: no command boundaries:", _e, file=_sys.stderr)
    finally:
        _sys.path.remove("@KTON_PY@")

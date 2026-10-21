# kton-trace.R — command boundaries for the recorder, without installing anything.
# Loads the functions of the ktonrec package from next to this file and starts them.
#
#   in ~/.Rprofile or via R_PROFILE_USER:
#     if (nzchar(Sys.getenv("KTON_TRACE_LOG"))) source("<repo>/recorder/r/kton-trace.R")
#
# Installed alternatively as a package:  R CMD INSTALL recorder/r/ktonrec ; ktonrec::start()
local({
  if (!nzchar(Sys.getenv("KTON_TRACE_LOG"))) return(invisible(NULL))
  # where this file lives: $KTON_R_HOME, else the source() call that is loading it
  here <- Sys.getenv("KTON_R_HOME")
  if (!nzchar(here)) {
    for (f in rev(sys.frames())) {
      if (!is.null(f$ofile) && grepl("kton-trace\\.R$", f$ofile)) {
        here <- dirname(normalizePath(f$ofile))
        break
      }
    }
  }
  if (!nzchar(here)) here <- dirname(Sys.getenv("KTON_TRACE_R", "recorder/r/kton-trace.R"))
  ns <- new.env(parent = baseenv())
  sys.source(file.path(here, "ktonrec", "R", "ktonrec.R"), envir = ns, keep.source = FALSE)
  ns$start()
  assign(".ktonrec_ns", ns, envir = globalenv())  # keeps the functions reachable (stop_recording)
  message("kton-trace aktiv: ", Sys.getenv("KTON_TRACE_LOG"))
})

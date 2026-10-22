# R profile for a session under the recorder (Positron: set as R_PROFILE_USER in
# positron.r.kernel.env). Loads the user's own ~/.Rprofile first, then the boundaries.
local({
  up <- path.expand("~/.Rprofile")
  if (file.exists(up)) sys.source(up, envir = globalenv())
})
if (nzchar(Sys.getenv("KTON_TRACE_LOG"))) {
  local({
    here <- Sys.getenv("KTON_R_HOME", file.path(path.expand("~"), ".local", "share", "kton", "r"))
    Sys.setenv(KTON_R_HOME = here)
    source(file.path(here, "kton-trace.R"))
  })
}

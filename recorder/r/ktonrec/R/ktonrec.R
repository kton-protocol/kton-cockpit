# ktonrec: command boundaries for the kton recorder.
#
# The shim (libktontrace.so) sees every file a process touches; it does not know where one
# R command ends and the next begins. ktonrec writes those boundaries into the same log:
#
#   TASK    after every top-level command (task callback), status ok|err
#   SOURCE  when source()/sys.source() runs a file, with its absolute path
#   ENV     once: R.home() and .libPaths(), so the watcher counts them as environment,
#           and the working directory at start as the project (in Positron: the open folder)
#   VARS    after a command: the variables of the global environment it created or changed
#
# Under Ark (Positron) the callback sees only Ark's own `.ark_last_value`, never the user's
# code, and it fires after errors too. Failures are marked by an options(error) hook, which R
# and Ark both run for an uncaught error only. A changed geterrmessage() is no signal under Ark:
# completions and help lookups while typing run try() and change it between two commands. It is
# used only when someone else owns options(error).
# Which file or chunk the code came from comes from the Positron extension (S6).

.ktonrec <- new.env(parent = emptyenv())

#' Path of the event log, or "" when recording is off.
log_path <- function() Sys.getenv("KTON_TRACE_LOG")

.paused <- function() file.exists(file.path(dirname(.ktonrec$log), "PAUSED"))

.line <- function(op, mode = "-", path = "-", extra = "-") {
  # recording switched off (PAUSED next to the log): no boundaries either, except the session's
  # own frame (ENV at start, END at the end) so it stays a session
  if (!op %in% c("ENV", "END") && .paused()) return(invisible())
  clean <- function(x) gsub("[\t\r\n]", " ", x)
  cat(sprintf("%.0f\t%d\t0\t%s\t%s\t%s\t0\t0\t0\t%s\n",
              as.numeric(Sys.time()) * 1e9, Sys.getpid(), op, mode, clean(path), clean(extra)),
      file = .ktonrec$log, append = TRUE)
}

.describe <- function(x) {
  cls <- paste(class(x), collapse = "/")
  d <- dim(x)
  shape <- if (!is.null(d)) paste(d, collapse = "x") else as.character(length(x))
  paste0(cls, ":", shape)
}

#' Variables of the global environment created or changed since the last call. Comparing
#' with identical() against the previous object is a pointer check for an unchanged object,
#' so this costs no copies; holding the previous version keeps it alive for one command.
snapshot <- function(env = globalenv()) {
  prev <- .ktonrec$prev
  if (is.null(prev)) prev <- .ktonrec$prev <- new.env(parent = emptyenv())
  now <- ls(env, all.names = FALSE)
  changed <- character()
  for (n in now) {
    obj <- get(n, envir = env, inherits = FALSE)
    if (!exists(n, envir = prev, inherits = FALSE) || !identical(get(n, envir = prev), obj)) {
      changed <- c(changed, n)
      assign(n, obj, envir = prev)
    }
  }
  removed <- setdiff(ls(prev, all.names = FALSE), now)
  if (length(removed)) rm(list = removed, envir = prev)
  vapply(changed, function(n) .describe(get(n, envir = env)), character(1))
}

.mark <- function(expr, value, ok, visible) {
  .ktonrec$n <- .ktonrec$n + 1L
  txt <- substr(paste(deparse(expr, width.cutoff = 500L), collapse = " "), 1L, 300L)
  if (isTRUE(.ktonrec$failed)) { # the hook has marked this command already (Ark calls us after it)
    .ktonrec$failed <- FALSE
    .ktonrec$n <- .ktonrec$n - 1L
    return(TRUE)
  }
  err <- geterrmessage()
  st <- if (!isTRUE(ok) || (!isTRUE(.ktonrec$hook) && !identical(err, .ktonrec$last_err))) "err" else "ok"
  .ktonrec$last_err <- err
  vars <- tryCatch(snapshot(), error = function(e) character())
  if (length(vars)) {
    v <- utils::head(vars, 50L)
    .line("VARS", "-", "-", paste(paste0(names(v), "=", v), collapse = ";"))
  }
  .line("TASK", st, as.character(.ktonrec$n), txt)
  TRUE
}

.src <- function(f) {
  if (!is.character(f) || length(f) != 1L) return(invisible())
  .line("SOURCE", "-", tryCatch(normalizePath(f, mustWork = FALSE), error = function(e) f))
}

#' Start writing boundaries into $KTON_TRACE_LOG. Does nothing when it is not set.
start <- function() {
  log <- log_path()
  if (!nzchar(log)) return(invisible(FALSE))
  if (isTRUE(.ktonrec$on)) return(invisible(TRUE))
  # Only the top R of a session writes boundaries. An R that a tool starts underneath (PsN,
  # callr, Rscript from system()) inherits R_PROFILE_USER and would otherwise write its own
  # TASK/ENV lines into its parent's log.
  owner <- Sys.getenv("KTON_TRACE_OWNER")
  if (nzchar(owner) && owner != as.character(Sys.getpid())) return(invisible(FALSE))
  Sys.setenv(KTON_TRACE_OWNER = Sys.getpid())
  .ktonrec$log <- log
  .ktonrec$n <- 0L
  .ktonrec$last_err <- geterrmessage()
  .ktonrec$prev <- NULL
  invisible(snapshot()) # what exists at start is not "changed by the first command"
  .line("ENV", "-", paste(unique(c(R.home(), .libPaths())), collapse = ","),
        paste0("project=", normalizePath(getwd(), mustWork = FALSE)))
  removeTaskCallback("ktonrec")
  addTaskCallback(.mark, name = "ktonrec")
  # Plain R calls task callbacks only after commands that succeeded; a failed command's file
  # access would land on the next one. options(error) runs at the failure. Only set when
  # nobody else did (Ark, the user): Ark reports failures through its own marker anyway.
  .ktonrec$hook <- is.null(getOption("error"))
  if (.ktonrec$hook) {
    options(error = function() {
      .ktonrec$n <- .ktonrec$n + 1L
      .ktonrec$last_err <- geterrmessage()
      .ktonrec$failed <- "tools:positron" %in% search() # Ark: its callback follows; plain R: none does
      .line("TASK", "err", as.character(.ktonrec$n), substr(gsub("\n", " ", geterrmessage()), 1L, 200L))
    })
  }
  src <- .src
  suppressMessages({
    trace(base::source, bquote(.(src)(file)), print = FALSE, where = baseenv())
    trace(base::sys.source, bquote(.(src)(file)), print = FALSE, where = baseenv())
  })
  reg.finalizer(.ktonrec, function(e) if (isTRUE(e$on)) .line("END"), onexit = TRUE)
  .ktonrec$on <- TRUE
  invisible(TRUE)
}

#' Stop writing boundaries and mark the end of the session.
stop_recording <- function() {
  if (!isTRUE(.ktonrec$on)) return(invisible(FALSE))
  removeTaskCallback("ktonrec")
  suppressMessages({
    untrace(base::source, where = baseenv())
    untrace(base::sys.source, where = baseenv())
  })
  .line("END")
  .ktonrec$on <- FALSE
  invisible(TRUE)
}

# ktonrec: command boundaries for the kton recorder.
#
# The shim (libktontrace.so) sees every file a process touches; it does not know where one
# R command ends and the next begins. ktonrec writes those boundaries into the same log:
#
#   TASK    after every top-level command (task callback), status ok|err
#   SOURCE  when source()/sys.source() runs a file, with its absolute path
#   ENV     once: R.home() and .libPaths(), so the watcher counts them as environment,
#           and the working directory at start as the project (in Positron: the open folder)
#   VARS    after a command: the variables of the global environment it created or changed, each
#           with the hash of its value (MH 21.11.: what is new or changed and needs saving), or
#           why it is not hashed (filtered by name, pattern, class or size; see track_options())
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
  # recording switched off (PAUSED next to the log): nothing but the session's own frame (ENV at
  # start, END at the end) so it stays a session, and a TASK marked paused
  if (!op %in% c("ENV", "END") && .paused()) {
    # a TASK stays a boundary, so what the shim logged before it saw PAUSED (it looks at most
    # once a second) is dropped instead of handed to the next command; the code is not written
    if (op != "TASK") return(invisible())
    mode <- "paused"
    extra <- "-"
  }
  # the shim's v2 escaping: a backslash, TAB, LF and CR as \\, \t, \n, \r
  clean <- function(x) {
    x <- gsub("\\", "\\\\", x, fixed = TRUE)
    x <- gsub("\t", "\\t", x, fixed = TRUE)
    x <- gsub("\n", "\\n", x, fixed = TRUE)
    gsub("\r", "\\r", x, fixed = TRUE)
  }
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

#' Which values are hashed: R option ktonrec.track, a list with
#'   exclude   names never hashed (character)
#'   pattern   a regular expression of names never hashed
#'   class     classes never hashed (e.g. "function", "ggplot")
#'   max_mb    values larger than this (object.size) are not hashed (default 200)
#' or the environment variable KTON_R_TRACK ("exclude=a,b;pattern=^tmp;class=function;max_mb=50"),
#' which the Positron extension sets from its setting. The option wins.
track_options <- function() {
  o <- getOption("ktonrec.track")
  if (is.null(o)) {
    o <- list()
    env <- Sys.getenv("KTON_R_TRACK")
    for (kv in strsplit(env, ";", fixed = TRUE)[[1]]) {
      p <- regexpr("=", kv, fixed = TRUE)
      if (p < 2) next
      k <- substr(kv, 1, p - 1)
      v <- substr(kv, p + 1, nchar(kv))
      o[[k]] <- if (k %in% c("exclude", "class")) strsplit(v, ",", fixed = TRUE)[[1]] else v
    }
  }
  list(exclude = as.character(o$exclude %||% character()), pattern = as.character(o$pattern %||% ""),
       class = as.character(o$class %||% character()), max_mb = as.numeric(o$max_mb %||% 200))
}
`%||%` <- function(a, b) if (is.null(a)) b else a

#' The hash of a value ("md5:<hex>" of its serialization, source references dropped), or why not
#' ("skip:<reason>"). Written to a temporary file and hashed there: base R hashes files only. The
#' temporary directory is the session's own; the recorder does not count it as the project's.
value_hash <- function(name, x, opt = track_options()) {
  if (name %in% opt$exclude) return("skip:excluded")
  if (nzchar(opt$pattern) && isTRUE(grepl(opt$pattern, name))) return("skip:pattern")
  if (length(opt$class) && any(class(x) %in% opt$class)) return("skip:class")
  size <- tryCatch(as.numeric(utils::object.size(x)), error = function(e) NA_real_)
  if (!is.na(size) && size > opt$max_mb * 1024 * 1024) return(sprintf("skip:size %.1f MB", size / 1024 / 1024))
  if (is.function(x)) x <- tryCatch(utils::removeSource(x), error = function(e) x)
  f <- tempfile("ktonrec-")
  on.exit(unlink(f), add = TRUE)
  ok <- tryCatch({
    con <- file(f, "wb")
    tryCatch(serialize(x, con, version = 3), finally = close(con)) # closed (flushed) before it is hashed
    TRUE
  }, error = function(e) FALSE)
  if (!ok) return("skip:unserializable")
  paste0("md5:", unname(tools::md5sum(f)))
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
  vars <- utils::head(tryCatch(snapshot(), error = function(e) character()), 50L)
  if (length(vars)) {
    opt <- tryCatch(track_options(), error = function(e) list(exclude = character(), pattern = "", class = character(), max_mb = 200))
    for (n in names(vars)) {
      h <- tryCatch(value_hash(n, get(n, envir = globalenv()), opt), error = function(e) "skip:error")
      vars[[n]] <- paste0(vars[[n]], "|", h)
    }
  }
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

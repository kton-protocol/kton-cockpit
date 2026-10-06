# Der Renderlauf der Paketdokumentation: Anforderungen (URS) und Spezifikation (FS) zu einem
# Dokument, in fester Reihenfolge. Läuft im gepinnten jam-r-Image, ohne Netz; dieselben Quellen
# ergeben dieselben Bytes.
args <- commandArgs(trailingOnly = TRUE)
out <- args[1]
abschnitt <- function(dir, titel) {
  files <- sort(list.files(dir, pattern = "\\.md$", full.names = TRUE), method = "radix")
  nr <- as.integer(sub("^[A-Z]+-([0-9]+).*$", "\\1", basename(files)))
  files <- files[order(nr)]
  teile <- vapply(files, function(f) {
    text <- readLines(f, encoding = "UTF-8", warn = FALSE)
    paste(c(paste0("### ", sub("\\.md$", "", basename(f))), "", text, ""), collapse = "\n")
  }, character(1))
  paste(c(paste0("## ", titel), "", teile), collapse = "\n")
}
doc <- c("# Raute — Dokumentation", "",
         abschnitt("requirements", "Anforderungen (URS)"),
         abschnitt("specification", "Spezifikation (FS)"))
con <- file(out, open = "w", encoding = "UTF-8")
writeLines(doc, con, sep = "\n")
close(con)

n <- as.integer(commandArgs(trailingOnly = TRUE)[1])
x <- readLines("daten.txt")
writeLines(paste0("B: ", head(x, n)), "b.txt")

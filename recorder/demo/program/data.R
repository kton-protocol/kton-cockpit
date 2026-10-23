# The studies' data, deterministic: run as `Rscript data.R <dir>`, writes
#   <dir>/thp101_pk.csv  datasets::Theoph as it is
#   <dir>/thp102_pk.csv  the same subjects fasted and fed, simulated from their individual fits
#   <dir>/cx201_pk.csv   datasets::Indometh, renamed as compound X
# Base R and nlme (a recommended package) only. Numbers are rounded, so the bytes are the same
# on every run and every machine with the same R.
suppressMessages(library(nlme))
out <- commandArgs(TRUE)[1]
dir.create(out, showWarnings = FALSE, recursive = TRUE)
w <- function(d, f) write.csv(d, file.path(out, f), row.names = FALSE)

th <- as.data.frame(Theoph)
th$Subject <- as.integer(as.character(th$Subject))
th <- th[order(th$Subject, th$Time), c("Subject", "Wt", "Dose", "Time", "conc")]
w(th, "thp101_pk.csv")

# THP-102: fasted as in THP-101, fed with half the absorption rate; same clearance and volume
fit <- coef(nlsList(conc ~ SSfol(Dose, Time, lKe, lKa, lCl), data = groupedData(conc ~ Time | Subject, data = th)))
set.seed(102)
times <- c(0, 0.25, 0.5, 1, 2, 3.5, 5, 7, 9, 12, 24)
rows <- list()
for (s in sort(unique(th$Subject))) {
  p <- fit[as.character(s), ]
  info <- th[th$Subject == s, ][1, ]
  for (food in c("fasted", "fed")) {
    ka <- exp(p$lKa) * if (food == "fed") 0.5 else 1
    ke <- exp(p$lKe); cl <- exp(p$lCl); v <- cl / ke
    c <- info$Dose * ka / (v * (ka - ke)) * (exp(-ke * times) - exp(-ka * times))
    c <- pmax(round(c * exp(rnorm(length(times), 0, 0.08)), 2), 0)
    rows[[length(rows) + 1]] <- data.frame(Subject = s, Wt = info$Wt, Dose = info$Dose,
      Period = if ((s %% 2 == 0) == (food == "fasted")) 1L else 2L, Food = food, Time = times, conc = c)
  }
}
w(do.call(rbind, rows), "thp102_pk.csv")

ix <- as.data.frame(Indometh)
ix$Subject <- as.integer(as.character(ix$Subject))
w(ix[order(ix$Subject, ix$time), c("Subject", "time", "conc")] |> setNames(c("Subject", "Time", "conc")), "cx201_pk.csv")

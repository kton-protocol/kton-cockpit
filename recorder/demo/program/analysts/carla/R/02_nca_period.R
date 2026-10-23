# NCA per subject and period (linear trapezoid): Cmax, Tmax, AUC to the last sample.
pk <- read.csv("derived/pk.csv")
nca <- do.call(rbind, lapply(split(pk, list(pk$Subject, pk$Food), drop = TRUE), function(d) {
  d <- d[order(d$Time), ]
  data.frame(Subject = d$Subject[1], Food = d$Food[1], Period = d$Period[1], Cmax = max(d$conc),
             Tmax = d$Time[which.max(d$conc)], AUClast = round(sum(diff(d$Time) * (head(d$conc, -1) + tail(d$conc, -1)) / 2), 3))
}))
nca <- nca[order(nca$Subject, nca$Food), ]
write.csv(nca, "derived/nca_period.csv", row.names = FALSE)
cat("NCA:", nrow(nca), "subject-periods\n")

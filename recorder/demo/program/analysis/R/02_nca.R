# Step 2: non-compartmental analysis per subject (SAP 1): Cmax, Tmax, AUC0-24 (trapezoidal).
pk <- read.csv("derived/pk.csv")
nca <- do.call(rbind, lapply(split(pk, pk$Subject), function(d) {
  d <- d[order(d$Time), ]
  data.frame(Subject = d$Subject[1], Cmax = max(d$conc), Tmax = d$Time[which.max(d$conc)],
             AUC = round(sum(diff(d$Time) * (head(d$conc, -1) + tail(d$conc, -1)) / 2), 2))
}))
write.csv(nca, "derived/nca.csv", row.names = FALSE)
print(summary(nca[, -1]))

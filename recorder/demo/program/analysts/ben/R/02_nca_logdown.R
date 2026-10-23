# NCA per subject with the linear-up / log-down trapezoid (the declining part is exponential):
# Cmax, Tmax, AUC to the last sample, and the terminal half-life from the last three samples.
pk <- read.csv("derived/pk.csv")
auc <- function(t, c) {
  s <- 0
  for (i in seq_along(t)[-1]) {
    dt <- t[i] - t[i - 1]
    s <- s + if (c[i] < c[i - 1] && c[i] > 0) dt * (c[i - 1] - c[i]) / log(c[i - 1] / c[i]) else dt * (c[i] + c[i - 1]) / 2
  }
  s
}
nca <- do.call(rbind, lapply(split(pk, pk$Subject), function(d) {
  d <- d[order(d$Time), ]
  last <- tail(d, 3)
  lz <- -coef(lm(log(conc) ~ Time, data = last))[2]
  data.frame(Subject = d$Subject[1], Cmax = max(d$conc), Tmax = d$Time[which.max(d$conc)],
             AUClast = round(auc(d$Time, d$conc), 3), thalf = round(log(2) / lz, 3))
}))
write.csv(nca, "derived/nca_logdown.csv", row.names = FALSE)
cat("NCA (log-down):", nrow(nca), "subjects\n")

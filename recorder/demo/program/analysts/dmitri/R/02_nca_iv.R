# NCA after an IV bolus: AUC to the last sample (linear trapezoid, back-extrapolated to time 0
# from the first two samples), terminal half-life from the last three, AUC to infinity, clearance.
pk <- read.csv("derived/pk.csv")
nca <- do.call(rbind, lapply(split(pk, pk$Subject), function(d) {
  d <- d[order(d$Time), ]
  c0 <- exp(log(d$conc[1]) - (log(d$conc[2]) - log(d$conc[1])) / (d$Time[2] - d$Time[1]) * d$Time[1])
  t <- c(0, d$Time); c <- c(c0, d$conc)
  auclast <- sum(diff(t) * (head(c, -1) + tail(c, -1)) / 2)
  lz <- -coef(lm(log(conc) ~ Time, data = tail(d, 3)))[[2]]
  aucinf <- auclast + tail(d$conc, 1) / lz
  data.frame(Subject = d$Subject[1], AUClast = round(auclast, 4), AUCinf = round(aucinf, 4),
             thalf = round(log(2) / lz, 3), CL = round(d$dose_mg[1] / aucinf, 3))
}))
write.csv(nca, "derived/nca.csv", row.names = FALSE)
cat("NCA:", nrow(nca), "subjects, median CL", median(nca$CL), "L/h\n")

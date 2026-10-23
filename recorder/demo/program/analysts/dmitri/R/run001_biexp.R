# run001: two compartments after an IV bolus (SSbiexp: two exponential phases), random effects on
# both amplitudes.
suppressMessages(library(nlme))
pk <- read.csv("derived/pk.csv")
d <- groupedData(conc ~ Time | Subject, data = pk)
fit <- nlme(conc ~ SSbiexp(Time, A1, lrc1, A2, lrc2), data = d,
            fixed = A1 + lrc1 + A2 + lrc2 ~ 1, random = pdDiag(A1 + A2 ~ 1),
            start = c(A1 = 2.8, lrc1 = 0.8, A2 = 0.6, lrc2 = -1.3))
est <- fixef(fit)
write.csv(data.frame(run = "run001-biexp", parameter = names(est), estimate = round(unname(est), 5),
                     AIC = round(AIC(fit), 3)), "derived/run001.csv", row.names = FALSE)
cat("run001 AIC", round(AIC(fit), 2), "\n")

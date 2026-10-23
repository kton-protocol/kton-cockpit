# run001: the base model. One compartment, first-order absorption and elimination (SSfol),
# random effects on Ka and Cl; clearance per kg of the weight-normalised dose.
suppressMessages(library(nlme))
pk <- read.csv("derived/pk.csv")
d <- groupedData(conc ~ Time | Subject, data = pk)
fit <- nlme(conc ~ SSfol(Dose, Time, lKe, lKa, lCl), data = d,
            fixed = lKe + lKa + lCl ~ 1, random = pdDiag(lKa + lCl ~ 1),
            start = c(lKe = -2.4, lKa = 0.45, lCl = -3.2))
est <- fixef(fit)
saveRDS(fit, "derived/run001.rds")
write.csv(data.frame(run = "run001-1cmt", parameter = names(est), estimate = round(unname(est), 5),
                     AIC = round(AIC(fit), 3)), "derived/run001.csv", row.names = FALSE)
cat("run001 AIC", round(AIC(fit), 2), "\n")

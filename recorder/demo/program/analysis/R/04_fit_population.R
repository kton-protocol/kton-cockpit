# Step 4: population model, the same structure with random effects on Ka and Cl (SAP 3).
suppressMessages(library(nlme))
pk <- read.csv("derived/pk.csv")
fit <- nlme(conc ~ SSfol(Dose, Time, lKe, lKa, lCl), data = groupedData(conc ~ Time | Subject, data = pk),
            fixed = lKe + lKa + lCl ~ 1, random = pdDiag(lKa + lCl ~ 1), start = c(-2.4, 0.45, -3.2))
saveRDS(fit, "derived/fit_population.rds")
re <- as.numeric(VarCorr(fit)[c("lKa", "lCl"), "StdDev"])
write.csv(data.frame(parameter = c("lKe", "lKa", "lCl", "omega_lKa", "omega_lCl", "sigma"),
                     estimate = round(c(fixef(fit), re, fit$sigma), 4)), "derived/fit_population.csv", row.names = FALSE)
print(fixef(fit))

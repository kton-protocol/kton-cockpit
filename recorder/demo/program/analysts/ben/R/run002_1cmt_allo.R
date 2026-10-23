# run002: run001 with weight as an allometric covariate on clearance, CL = CL70 * (WT/70)^0.75,
# on the absolute dose (Dose is mg/kg). Starts from run001's estimates.
suppressMessages(library(nlme))
pk <- read.csv("derived/pk.csv")
base <- read.csv("derived/run001.csv")
b <- setNames(base$estimate, base$parameter)
d <- groupedData(conc ~ Time | Subject, data = pk)
cl70 <- log(exp(b[["lCl"]]) * 70)        # per kg of dose → absolute, for 70 kg
v <- log(exp(b[["lCl"]]) / exp(b[["lKe"]]) * 70)
fit <- nlme(conc ~ (Dose * Wt) * exp(lKa) / (exp(lV) * (exp(lKa) - exp(lCl70) * (Wt / 70)^0.75 / exp(lV))) *
              (exp(-exp(lCl70) * (Wt / 70)^0.75 / exp(lV) * Time) - exp(-exp(lKa) * Time)),
            data = d, fixed = lKa + lCl70 + lV ~ 1, random = pdDiag(lKa + lCl70 ~ 1),
            start = c(lKa = b[["lKa"]], lCl70 = cl70, lV = v))
est <- fixef(fit)
saveRDS(fit, "derived/run002.rds")
write.csv(data.frame(run = "run002-1cmt-allo-wt", parameter = names(est), estimate = round(unname(est), 5),
                     AIC = round(AIC(fit), 3)), "derived/run002.csv", row.names = FALSE)
cat("run002 AIC", round(AIC(fit), 2), "\n")

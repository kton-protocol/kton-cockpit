# Step 7: steady state at 100, 200 and 400 mg twice daily of the immediate-release formulation
# studied in THP-101, for a 70 kg patient, from the population model: the fan-out, one file per
# dose. The weight effect enters only if step 6 found it significant (SAP 5).
pop <- read.csv("derived/fit_population.csv")
cov <- read.csv("derived/covariate_weight_cl.csv")
est <- setNames(pop$estimate, pop$parameter)
ke <- exp(est["lKe"]); ka <- exp(est["lKa"])
# SSfol's clearance is per kg (the dose is in mg/kg); a used weight effect shifts it, then x 70 kg
shift <- if (isTRUE(as.logical(cov$used))) cov$slope_per_kg * (70 - cov$reference_weight) else 0
cl70 <- (exp(est["lCl"]) + shift) * 70
v <- cl70 / ke; tau <- 12; t <- seq(0, tau, by = 0.25)
for (dose in c(100, 200, 400)) {
  c <- dose * ka / (v * (ka - ke)) * (exp(-ke * t) / (1 - exp(-ke * tau)) - exp(-ka * t) / (1 - exp(-ka * tau)))
  write.csv(data.frame(dose_mg = dose, time_h = t, conc = round(c, 3)), sprintf("derived/sim_%dmg.csv", dose), row.names = FALSE)
}
cat("simulated 100, 200, 400 mg q12h at steady state;", if (shift == 0) "no weight effect\n" else "with the weight effect\n")

# Step 7: steady state at 100, 200 and 400 mg twice daily for a 70 kg patient, from the population
# model and the weight effect: the fan-out, one file per dose.
pop <- read.csv("derived/fit_population.csv")
cov <- read.csv("derived/covariate_weight_cl.csv")
est <- setNames(pop$estimate, pop$parameter)
ke <- exp(est["lKe"]); ka <- exp(est["lKa"])
# SSfol's clearance is per kg (the dose is in mg/kg); the weight effect shifts it, then x 70 kg
cl70 <- (exp(est["lCl"]) + cov$slope_per_kg * (70 - cov$reference_weight)) * 70
v <- cl70 / ke; tau <- 12; t <- seq(0, tau, by = 0.25)
for (dose in c(100, 200, 400)) {
  c <- dose * ka / (v * (ka - ke)) * (exp(-ke * t) / (1 - exp(-ke * tau)) - exp(-ka * t) / (1 - exp(-ka * tau)))
  write.csv(data.frame(dose_mg = dose, time_h = t, conc = round(c, 3)), sprintf("derived/sim_%dmg.csv", dose), row.names = FALSE)
}
cat("simulated 100, 200, 400 mg q12h at steady state\n")

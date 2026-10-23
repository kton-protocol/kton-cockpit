# Step 8: exposure at steady state against the target product profile (trough >= 5 mg/L minimum,
# 8 target; peak < 20 mg/L maximum, 15 target): the fan-in of the three simulations.
sims <- do.call(rbind, lapply(c(100, 200, 400), function(d) read.csv(sprintf("derived/sim_%dmg.csv", d))))
ex <- do.call(rbind, lapply(split(sims, sims$dose_mg), function(d)
  data.frame(dose_mg = d$dose_mg[1], trough = min(d$conc), peak = max(d$conc))))
ex$meets_minimum <- ex$trough >= 5 & ex$peak < 20
ex$meets_target <- ex$trough >= 8 & ex$peak < 15
write.csv(ex, "derived/exposure_vs_tpp.csv", row.names = FALSE)
png("figures/exposure.png", width = 800, height = 500)
plot(NA, xlim = c(0, 12), ylim = c(0, max(sims$conc) * 1.1), xlab = "time after dose (h)", ylab = "concentration (mg/L)",
     main = "Steady state, twice daily, 70 kg")
rect(0, 5, 12, 20, col = "#e8f4e8", border = NA); abline(h = c(8, 15), lty = 2)
for (i in seq_along(split(sims, sims$dose_mg))) { d <- split(sims, sims$dose_mg)[[i]]; lines(d$time_h, d$conc, lwd = 2, col = i) }
legend("topright", legend = paste(names(split(sims, sims$dose_mg)), "mg"), col = 1:3, lwd = 2)
dev.off()
print(ex)

# Step 9: goodness of fit of the population model: observed against predicted, from the fit's own
# data (groupedData orders the subjects its own way).
suppressMessages(library(nlme))
fit <- readRDS("derived/fit_population.rds")
pk <- read.csv("derived/pk.csv") # getData() evaluates the fit's data expression, which names pk
d <- getData(fit)
png("figures/gof.png", width = 800, height = 400)
par(mfrow = c(1, 2))
plot(fitted(fit), d$conc, xlab = "individual prediction", ylab = "observed", main = "Observed vs predicted"); abline(0, 1)
plot(d$Time, resid(fit, type = "normalized"), xlab = "time (h)", ylab = "normalised residual", main = "Residuals over time"); abline(h = 0)
dev.off()
cat("gof.png\n")

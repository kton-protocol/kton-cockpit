# Goodness of fit of the final model run001: observed against individual prediction, and
# normalised residuals over time.
suppressMessages(library(nlme))
fit <- readRDS("derived/run001.rds")
pk <- read.csv("derived/pk.csv")
d <- groupedData(conc ~ Time | Subject, data = pk) # the fit's data expression names d
g <- getData(fit)
png("figures/gof_run001.png", width = 800, height = 400)
par(mfrow = c(1, 2))
plot(fitted(fit), g$conc, xlab = "individual prediction", ylab = "observed", main = "run001: observed vs predicted"); abline(0, 1)
plot(g$Time, resid(fit, type = "normalized"), xlab = "time (h)", ylab = "normalised residual", main = "run001: residuals"); abline(h = 0)
dev.off()
cat("gof_run001.png\n")

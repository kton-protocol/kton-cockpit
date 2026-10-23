# Food effect: geometric mean ratio fed/fasted of AUClast and Cmax with a 90 % confidence interval,
# from the within-subject log differences (crossover, no period effect assumed).
nca <- read.csv("derived/nca_period.csv")
w <- reshape(nca[, c("Subject", "Food", "AUClast", "Cmax")], idvar = "Subject", timevar = "Food", direction = "wide")
res <- do.call(rbind, lapply(c("AUClast", "Cmax"), function(m) {
  d <- log(w[[paste0(m, ".fed")]]) - log(w[[paste0(m, ".fasted")]])
  n <- length(d); se <- sd(d) / sqrt(n); q <- qt(0.95, n - 1)
  data.frame(metric = m, n = n, GMR = round(exp(mean(d)), 4), lower90 = round(exp(mean(d) - q * se), 4), upper90 = round(exp(mean(d) + q * se), 4))
}))
write.csv(res, "derived/food_effect.csv", row.names = FALSE)
print(res)

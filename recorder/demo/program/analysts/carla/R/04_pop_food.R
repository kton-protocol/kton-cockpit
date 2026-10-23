# Population model: one compartment (SSfol) with food on the absorption rate, random effects on
# Ka and Cl; both periods together.
suppressMessages(library(nlme))
pk <- read.csv("derived/pk.csv")
pk$Food <- factor(pk$Food, levels = c("fasted", "fed"))
d <- groupedData(conc ~ Time | Subject, data = pk)
fit <- nlme(conc ~ SSfol(Dose, Time, lKe, lKa, lCl), data = d,
            fixed = list(lKe ~ 1, lKa ~ Food, lCl ~ 1), random = pdDiag(lKa + lCl ~ 1),
            start = c(lKe = -2.4, lKa = 0.45, lKa.fed = -0.7, lCl = -3.2))
est <- fixef(fit)
write.csv(data.frame(run = "pop-food-on-ka", parameter = names(est), estimate = round(unname(est), 5),
                     AIC = round(AIC(fit), 3)), "derived/pop_food.csv", row.names = FALSE)
cat("Ka fed/fasted:", round(exp(est[["lKa.Foodfed"]]), 3), "\n")

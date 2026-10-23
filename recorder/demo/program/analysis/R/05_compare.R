# Step 5: individual vs population model (SAP 4): the diamond joins here.
suppressMessages(library(nlme))
ind <- readRDS("derived/fit_individual.rds")
pop <- readRDS("derived/fit_population.rds")
ll_ind <- sum(sapply(ind, logLik)); k_ind <- 3 * length(ind) + length(ind)
cmp <- data.frame(model = c("individual (nlsList)", "population (nlme)"),
                  parameters = c(k_ind, length(fixef(pop)) + 3),
                  AIC = round(c(-2 * ll_ind + 2 * k_ind, AIC(pop)), 1))
# the SAP: the population model is used for simulation (individual fits say nothing about a new
# patient); the AIC is reported, the individual fits are bound to win it with 4 parameters each
cmp$used_for_simulation <- cmp$model == "population (nlme)"
write.csv(cmp, "derived/model_comparison.csv", row.names = FALSE)
print(cmp)

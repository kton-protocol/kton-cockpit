# Step 3: individual fits, one compartment with first-order absorption (SAP 2).
suppressMessages(library(nlme))
pk <- read.csv("derived/pk.csv")
fits <- nlsList(conc ~ SSfol(Dose, Time, lKe, lKa, lCl), data = groupedData(conc ~ Time | Subject, data = pk))
saveRDS(fits, "derived/fit_individual.rds")
p <- coef(fits)
p <- data.frame(Subject = as.integer(rownames(p)), round(p, 4), CL = round(exp(p$lCl), 4))
write.csv(p[order(p$Subject), ], "derived/fit_individual.csv", row.names = FALSE)
print(head(p))

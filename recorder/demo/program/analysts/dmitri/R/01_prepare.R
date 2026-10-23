# Step 1: the checked-out CX-201 data with the dose of the protocol (25 mg as an intravenous bolus).
pk <- read.csv("data/cx201_pk.csv")
pk$dose_mg <- 25
write.csv(pk, "derived/pk.csv", row.names = FALSE)
cat("pk.csv:", nrow(pk), "samples,", length(unique(pk$Subject)), "subjects\n")

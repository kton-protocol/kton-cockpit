# Step 1: the checked-out study data, prepared for analysis: absolute dose in mg, one row per
# sample, and one row per subject with its covariates.
pk <- read.csv("data/thp101_pk.csv")
pk$dose_mg <- round(pk$Dose * pk$Wt, 1)
write.csv(pk, "derived/pk.csv", row.names = FALSE)
subjects <- unique(pk[, c("Subject", "Wt", "Dose", "dose_mg")])
write.csv(subjects, "derived/subjects.csv", row.names = FALSE)
cat("pk.csv:", nrow(pk), "samples,", nrow(subjects), "subjects\n")

# Step 1, adapted from the THP-101 analyses' 01_prepare.R for the crossover: the checked-out study
# data, absolute dose in mg, one row per sample, food and period as factors, one row per subject.
pk <- read.csv("data/thp102_pk.csv")
pk$dose_mg <- round(pk$Dose * pk$Wt, 1)
pk$Food <- factor(pk$Food, levels = c("fasted", "fed"))
write.csv(pk, "derived/pk.csv", row.names = FALSE)
subjects <- unique(pk[, c("Subject", "Wt", "Dose", "dose_mg")])
write.csv(subjects, "derived/subjects.csv", row.names = FALSE)
cat("pk.csv:", nrow(pk), "samples,", nrow(subjects), "subjects, 2 periods\n")

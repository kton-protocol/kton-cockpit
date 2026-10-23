"""Step 6 (Python): weight on clearance (SAP 5): regression of individual clearance (L/h per kg
of dose unit) on body weight, from the individual fits and the subjects' covariates."""
import csv

cl = {r["Subject"]: float(r["CL"]) for r in csv.DictReader(open("derived/fit_individual.csv"))}
wt = {r["Subject"]: float(r["Wt"]) for r in csv.DictReader(open("derived/subjects.csv"))}
xs = [wt[s] for s in sorted(cl, key=int)]
ys = [cl[s] for s in sorted(cl, key=int)]
n = len(xs)
mx, my = sum(xs) / n, sum(ys) / n
sxx = sum((x - mx) ** 2 for x in xs)
slope = sum((x - mx) * (y - my) for x, y in zip(xs, ys)) / sxx
r = slope * (sxx / sum((y - my) ** 2 for y in ys)) ** 0.5
with open("derived/covariate_weight_cl.csv", "w", newline="") as f:
    w = csv.writer(f)
    w.writerow(["covariate", "on", "slope_per_kg", "r", "n", "reference_weight"])
    w.writerow(["Wt", "CL", round(slope, 6), round(r, 3), n, round(mx, 1)])
print(f"CL ~ Wt: slope {slope:.6f} per kg, r = {r:.2f}")

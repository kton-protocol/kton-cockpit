"""Step 6 (Python): weight on clearance (SAP 5): regression of individual clearance (L/h per kg
of dose unit) on body weight, from the individual fits and the subjects' covariates. The effect is
used for simulation only if it is significant (SAP 5: two-sided p < 0.05 for the slope)."""
import csv
import math

cl = {r["Subject"]: float(r["CL"]) for r in csv.DictReader(open("derived/fit_individual.csv"))}
wt = {r["Subject"]: float(r["Wt"]) for r in csv.DictReader(open("derived/subjects.csv"))}
xs = [wt[s] for s in sorted(cl, key=int)]
ys = [cl[s] for s in sorted(cl, key=int)]
n = len(xs)
mx, my = sum(xs) / n, sum(ys) / n
sxx = sum((x - mx) ** 2 for x in xs)
slope = sum((x - mx) * (y - my) for x, y in zip(xs, ys)) / sxx
r = slope * (sxx / sum((y - my) ** 2 for y in ys)) ** 0.5


def t_sf2(t, df, steps=20000):
    """two-sided p of Student's t, by integrating the density from 0 to |t| (no scipy needed)"""
    c = math.exp(math.lgamma((df + 1) / 2) - math.lgamma(df / 2)) / math.sqrt(df * math.pi)  # gamma overflows from df ~ 343
    f = lambda x: c * (1 + x * x / df) ** (-(df + 1) / 2)
    h = abs(t) / steps
    area = h / 3 * (f(0) + f(abs(t)) + sum((4 if i % 2 else 2) * f(i * h) for i in range(1, steps)))
    return max(0.0, 1 - 2 * area)


t = r * math.sqrt((n - 2) / (1 - r * r))
p = t_sf2(t, n - 2)
use = p < 0.05
with open("derived/covariate_weight_cl.csv", "w", newline="") as f:
    w = csv.writer(f)
    w.writerow(["covariate", "on", "slope_per_kg", "r", "n", "p", "used", "reference_weight"])
    w.writerow(["Wt", "CL", round(slope, 6), round(r, 3), n, round(p, 3), str(use).upper(), round(mx, 1)])
print(f"CL ~ Wt: slope {slope:.6f} per kg, r = {r:.2f}, p = {p:.2f}: {'used' if use else 'not used'} for simulation")

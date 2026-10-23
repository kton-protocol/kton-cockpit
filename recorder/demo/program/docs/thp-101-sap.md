# Study THP-101 — statistical analysis plan (synopsis)

*Fictional example for the kton recorder.*

1. Non-compartmental analysis per subject: Cmax, Tmax, AUC by the trapezoidal rule.
2. Individual fits: one compartment with first-order absorption and elimination
   (`SSfol`: log Ke, log Ka, log Cl).
3. Population model: the same structure with random effects on Ka and Cl (`nlme`).
4. Model comparison by AIC, reported. The population model is used for simulation: individual fits
   say nothing about a new patient.
5. Weight on clearance: examined graphically and by regression of individual clearances.

# Study THP-101 — protocol (synopsis)

*Fictional example for the kton recorder. Part of Project Theophylline. The data are R's
`datasets::Theoph` (Boeckmann, Sheiner and Beal, 1994).*

- **Design:** open-label, single oral dose of theophylline, 12 healthy adult volunteers.
- **Dose:** 320 mg, reported per kg body weight (`Dose`, mg/kg).
- **Sampling:** 11 samples from pre-dose to 24 h (`Time`, h); serum concentration (`conc`, mg/L).
- **Covariates:** body weight (`Wt`, kg).
- **Data file:** `data/thp101_pk.csv`, one row per sample:
  `Subject, Wt, Dose, Time, conc`.

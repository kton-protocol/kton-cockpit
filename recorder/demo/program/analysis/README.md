# Analysis THP-101: population PK of theophylline

*Fictional example for the kton recorder.* This repository is where the analysis happens. It
knows nothing about the programme, the project or the study: it federates their repositories
(`cockpit federation`) and checks out a dataset. The recorder finds the context from where the
data comes from.

```
data/thp101_pk.csv  (checked out from study THP-101)
  └ R/01_prepare.R → derived/pk.csv, subjects.csv
      ├ R/02_nca.R → nca.csv
      ├ R/03_fit_individual.R → fit_individual.* ──┬ R/05_compare.R → model_comparison.csv   (a diamond)
      ├ R/04_fit_population.R → fit_population.* ──┤
      │                                            └ R/09_gof.R → figures/gof.png
      └ py/06_covariates.py (fit_individual + subjects) → covariate_weight_cl.csv   (R → Python)
          └ R/07_simulate.R (+ population fit) → sim_100mg / sim_200mg / sim_400mg.csv   (a fan-out)
              └ R/08_exposure.R → exposure_vs_tpp.csv, figures/exposure.png            (a fan-in)
report/report.qmd: labelled R chunks over derived/ and figures/ (rendered with knitr, e.g. in jam-r)
```

## In Positron (WSL window, this folder open, recording on)

1. **Check out the data** (terminal):
   `./checkout.sh ../program-respiratory/project-theophylline/study-thp-101 data/thp101_pk.csv`.
   It copies the file and asks the cockpit whose record it is (the study's).
2. **R console:** `source("R/01_prepare.R")`, then `R/02_nca.R`, `R/03_fit_individual.R` and
   `R/04_fit_population.R` the same way. Or run lines by hand: the recorder sees both.
3. **R, sourced:** `source("R/05_compare.R")`: the two models meet.
4. **Python console:** run `py/06_covariates.py`.
5. **R:** `source("R/07_simulate.R")`, `source("R/08_exposure.R")`, `source("R/09_gof.R")`.
6. Open the story of `figures/exposure.png` or `derived/exposure_vs_tpp.csv`: the whole way back to
   the study's dataset. Reproduce it. Describe the steps and publish them.

All at once from a terminal: `./run_all.sh` (after step 1).

## Later

- **Study THP-102** (fed vs fasted) can run through the same steps: the place where a workflow
  (a ray extracted from these steps, with the dataset as its hole) pays off.
- **The report** needs knitr: render it in the jam-r image.

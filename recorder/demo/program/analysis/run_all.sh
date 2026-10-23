#!/bin/sh
# The whole analysis from a terminal, in order. In Positron, run the steps one by one instead
# (README.md), so the recorder sees consoles, scripts and Python as they are used.
set -e
cd "$(dirname "$0")"
Rscript R/01_prepare.R
Rscript R/02_nca.R
Rscript R/03_fit_individual.R
Rscript R/04_fit_population.R
Rscript R/05_compare.R
python3 py/06_covariates.py
Rscript R/07_simulate.R
Rscript R/08_exposure.R
Rscript R/09_gof.R

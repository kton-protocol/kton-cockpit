#!/bin/sh
# A simulated working day on a PK project, recorded: four R sessions under the recorder and a
# hand edit in between. Afterwards `ktonwatch serve` shows it ("Alle Sitzungen" of the project).
#
#   PROJ=/path/to/arbeitstag-pk sh recorder/demo/arbeitstag.sh
#
# Needs: the installed recorder and cockpit (recorder/install.sh), R with haven, NONMEM (nmfe75), PsN
# (execute), bbi, PsN's pharmpy venv, quarto (Positron's bundled one is found).
set -e
PROJ=${PROJ:?set PROJ to the project directory to create}
SHARED=${SHARED:-$(dirname "$PROJ")/shared-drive}
NM=${NM:-$HOME/.local/opt/nm751}
PY=${PY:-$HOME/.local/opt/psn/PsN_5_7_2/pyvenv/bin/python}
QUARTO=${QUARTO:-$(command -v quarto || ls "$HOME"/.positron-server/bin/*/quarto/bin/quarto 2>/dev/null | head -1)}
KR="$HOME/.local/share/kton/r/kton-R"
[ -x "$HOME/.local/lib/kton/libktontrace.so" ] || [ -f "$HOME/.local/lib/kton/libktontrace.so" ] || { echo "erst recorder/install.sh" >&2; exit 1; }

say() { printf '\n== %s\n' "$*"; }

# ---------------------------------------------------------------- setup (not recorded)
say "Projekt anlegen: $PROJ"
rm -rf "$PROJ" "$SHARED"
mkdir -p "$PROJ/raw" "$PROJ/derived" "$PROJ/R" "$PROJ/model" "$PROJ/figures" "$PROJ/tables" "$PROJ/report" "$SHARED"
cd "$PROJ"
# a cockpit participant repo in local mode (ADR-004), so observations can be published by hand
COCKPIT=${COCKPIT:-$HOME/.local/bin/cockpit}
if [ -x "$COCKPIT" ]; then "$COCKPIT" init >/dev/null && "$COCKPIT" keygen analystin >/dev/null; fi
# the raw data the data manager delivered: THEOPP as CSV ("." = missing), dosing as SAS transport
awk 'BEGIN{print "ID,DOSE,TIME,CP,WT"} NF==5{print $1","$2","$3","$4","$5}' "$NM/run/THEOPP" > raw/theopp_raw.csv
Rscript -e 'd <- read.csv("raw/theopp_raw.csv", na.strings="."); d <- d[!is.na(d$DOSE), c("ID","DOSE","WT")]; haven::write_xpt(d, "raw/dosing.xpt")' >/dev/null
# lab values on a shared drive, outside the project
printf 'ID,ALT,CREAT\n' > "$SHARED/lab_values.csv"
for i in $(seq 1 12); do printf '%d,%d,%.2f\n' "$i" $((20 + i)) "$(echo "0.8 + $i / 100" | bc -l)" >> "$SHARED/lab_values.csv"; done

cat > R/explore.R <<'R'
png("figures/conc_time.png", width = 800, height = 500)
plot(CP ~ TIME, data = ana, col = ana$ID, pch = 19, main = "Konzentration über Zeit")
dev.off()
R
cat > R/nca.R <<'R'
ana <- read.csv("derived/analysis.csv")
obs <- ana[!is.na(ana$CP), ]
auc <- sapply(split(obs, obs$ID), function(d) { d <- d[order(d$TIME), ]; sum(diff(d$TIME) * (head(d$CP, -1) + tail(d$CP, -1)) / 2) })
cmax <- tapply(obs$CP, obs$ID, max)
nca <- data.frame(ID = as.integer(names(auc)), AUC = round(auc, 2), CMAX = cmax[names(auc)])
write.csv(nca, "tables/nca.csv", row.names = FALSE)
png("figures/nca_auc.png", width = 600, height = 400); barplot(nca$AUC, names.arg = nca$ID, main = "AUC je Proband"); dev.off()
R
cat > R/summarise.py <<'PY'
"""Reads NONMEM results with pharmpy and writes a short summary."""
import json, sys
from pharmpy.tools import read_modelfit_results
res = read_modelfit_results(sys.argv[1])
json.dump({"ofv": res.ofv, "estimates": {k: float(v) for k, v in res.parameter_estimates.items()}}, open(sys.argv[2], "w"), indent=1)
PY
# control streams: run1 for nmfe, run2 for PsN, run3 for bbi (bbi runs in a subdirectory)
sed 's/^\$DATA *THEOPP/$DATA nm_data.dat/; s/^\$TABLE \(.*\)$/$TABLE \1 FILE=sdtab1 NOPRINT ONEHEADER/' "$NM/run/CONTROL5" > model/run1.ctl
sed 's/^\$PROB .*/$PROB  THEOPHYLLINE   VARIANT FOR PSN/' model/run1.ctl > model/run2.mod
sed 's/^\$DATA nm_data.dat/$DATA ..\/nm_data.dat/; s/^\$PROB .*/$PROB  THEOPHYLLINE   VARIANT FOR BBI/' model/run1.ctl > model/run3.ctl
(cd model && bbi init --dir "$(dirname "$NM")" >/dev/null)
cat > report/bericht.qmd <<'Q'
---
title: "Theophyllin – Tagesbericht"
format: html
---

```{r}
nca <- read.csv("../tables/nca.csv")
knitr::kable(nca)
```

```{r}
fit <- jsonlite::fromJSON("../model/run1_summary.json")
cat("OFV:", fit$ofv)
```

![Konzentration](../figures/conc_time.png)
Q
Rscript -e 'if (!requireNamespace("jsonlite", quietly = TRUE)) quit(status = 1)' || sed -i 's/fit <- jsonlite::fromJSON("..\/model\/run1_summary.json")/fit <- list(ofv = readLines("..\/model\/run1_summary.json"))/' report/bericht.qmd

run() { # $1 label, $2 script: one R session under the recorder, like `Rscript $2`
  say "$1"
  KTON_TRACE_HASH="$PROJ" SHARED="$SHARED" PY="$PY" QUARTO="$QUARTO" sh "$KR" --no-save -q -f "$2" > ".$(basename "$2").out" 2>&1 || true
  tail -n 2 ".$(basename "$2").out"
}

# ---------------------------------------------------------------- the day's scripts
cat > R/01_daten.R <<'R'
pk <- read.csv("raw/theopp_raw.csv", na.strings = ".")
dose <- haven::read_xpt("raw/dosing.xpt")
lab <- read.csv(file.path(Sys.getenv("SHARED", "../shared-drive"), "lab_values.csv"))
x <- try(read.csv("raw/theopp_raw.cvs"), silent = TRUE)
con <- try(socketConnection("192.0.2.1", 5432, timeout = 1), silent = TRUE)
ana <- merge(pk, lab, by = "ID", all.x = TRUE); ana$CP_ugL <- ana$CP * 1000
write.csv(ana, "derived/analysis.csv", row.names = FALSE)
source("R/explore.R")
R
cat > R/02_nca.R <<'R'
ana <- read.csv("derived/analysis.csv")
ana$CP_ugL <- NULL
write.csv(ana, "derived/analysis.csv", row.names = FALSE)
dir.create("derived/subjects", showWarnings = FALSE); invisible(lapply(split(ana, ana$ID), function(d) write.csv(d, sprintf("derived/subjects/%02d.csv", d$ID[1]), row.names = FALSE)))
n_obs <- unlist(parallel::mclapply(list.files("derived/subjects", full.names = TRUE), function(f) sum(!is.na(read.csv(f)$CP)), mc.cores = 4))
source("R/nca.R")
R
cat > R/03_modell.R <<'R'
ana <- read.csv("derived/analysis.csv")
write.table(ana[, c("ID", "DOSE", "TIME", "CP", "WT")], "model/nm_data.dat", na = ".", row.names = FALSE, col.names = FALSE)
setwd("model"); rc1 <- system("nmfe75 run1.ctl run1.lst > nmfe_run1.out 2>&1"); setwd("..")
setwd("model"); rc2 <- system("execute run2.mod -directory=run2_psn > psn_run2.out 2>&1"); setwd("..")
setwd("model"); rc3 <- system("bbi nonmem run local run3.ctl > bbi_run3.out 2>&1"); setwd("..")
rc4 <- system2(Sys.getenv("PY", path.expand("~/.local/opt/psn/PsN_5_7_2/pyvenv/bin/python")), c("R/summarise.py", "model/run1.ctl", "model/run1_summary.json"))
R
cat > R/04_bericht.R <<'R'
quarto <- Sys.getenv("QUARTO", Sys.glob(path.expand("~/.positron-server/bin/*/quarto/bin/quarto"))[1])
rc <- system(paste(shQuote(quarto), "render report/bericht.qmd --quiet"))
R

run "08:30 Datenaufbereitung (Rscript R/01_daten.R)" R/01_daten.R

# ---------------------------------------------------------------- Excel, not recorded
say "dazwischen: analysis.csv von Hand in Excel korrigiert (nicht aufgezeichnet)"
python3 -I - <<'PY'
import csv
rows = list(csv.reader(open("derived/analysis.csv")))
rows[3][3] = "2.85"   # a "corrected" concentration
csv.writer(open("derived/analysis.csv", "w", newline="")).writerows(rows)
PY

run "10:15 Korrektur & NCA (R/02_nca.R)" R/02_nca.R
run "13:00 Modellierung: nmfe75, PsN, bbi, pharmpy (R/03_modell.R)" R/03_modell.R
run "16:00 Bericht (R/04_bericht.R)" R/04_bericht.R

# ---------------------------------------------------------------- 17:30 ad hoc, in the console
say "17:30 ad hoc in der Konsole: etwas für eine Kollegin"
cat > .adhoc.txt <<'R'
nca <- read.csv("tables/nca.csv")
summary(nca)
hist(nca$AUC)
top <- nca[nca$AUC > median(nca$AUC), ]
alt <- read.csv("tables/nca_alt.csv")
top$AUC_h <- round(top$AUC / 24, 2)
write.csv(top, "fuer_kollegin.csv", row.names = FALSE)
R
KTON_TRACE_HASH="$PROJ" sh "$KR" --no-save -q --interactive < .adhoc.txt > .adhoc.out 2>&1 || true
tail -n 2 .adhoc.out

say "fertig. Ergebnisse:"
grep -h "OBJECTIVE FUNCTION VALUE WITHOUT" model/run1.lst model/run2.lst model/run3/run3.lst 2>/dev/null | sed 's/^/   /' || true
ls report/ tables/ figures/ | sed 's/^/   /'

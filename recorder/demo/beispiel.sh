#!/bin/sh
# A small population-PK analysis of datasets::Theoph, recorded like a shell session: data,
# QC, EDA, two NONMEM models, model comparison, goodness of fit, a Quarto report. Every step
# is one shell command; ktonwatch tells it command by command (log shell-<time>.tsv).
#
#   PROJ=/path/to/beispiel-theo sh recorder/demo/beispiel.sh
#
# Built to be reproduced (kton-repro, or "Reproduzieren" in Positron): base R only, numbers in
# tables rounded to 4 significant digits, Quarto called from the shell rather than by a path
# inside R. Needs the installed recorder (recorder/install.sh), R, NONMEM (nmfe75), quarto.
set -e
PROJ=${PROJ:?set PROJ to the project directory to create}
SHIM="$HOME/.local/lib/kton/libktontrace.so"
TRACE=${XDG_CACHE_HOME:-$HOME/.cache}/kton-cockpit/trace
QUARTO_BIN=$(dirname "$(command -v quarto || ls "$HOME"/.positron-server/bin/*/quarto/bin/quarto 2>/dev/null | head -1)")
[ -f "$SHIM" ] || { echo "erst recorder/install.sh" >&2; exit 1; }
command -v nmfe75 >/dev/null || { echo "nmfe75 fehlt im PATH" >&2; exit 1; }

say() { printf '\n== %s\n' "$*"; }

# ---------------------------------------------------------------- setup (not recorded)
say "Projekt anlegen: $PROJ"
rm -rf "$PROJ"
mkdir -p "$PROJ"/R "$PROJ"/raw "$PROJ"/data "$PROJ"/qc "$PROJ"/tables "$PROJ"/figures "$PROJ"/models "$PROJ"/report
cd "$PROJ"
COCKPIT=${COCKPIT:-$HOME/.local/bin/cockpit}
if [ -x "$COCKPIT" ]; then "$COCKPIT" init >/dev/null && "$COCKPIT" keygen analystin >/dev/null; fi

cat > R/01_daten.R <<'R'
# Rohdaten und NONMEM-Datensatz aus datasets::Theoph
raw <- as.data.frame(datasets::Theoph)
raw$Subject <- as.integer(as.character(raw$Subject))
raw <- raw[order(raw$Subject, raw$Time), ]
write.csv(raw, "raw/theoph.csv", row.names = FALSE)

d <- read.csv("raw/theoph.csv")
dose <- unique(d[, c("Subject", "Wt", "Dose")])
dosing <- data.frame(ID = dose$Subject, TIME = 0, AMT = round(dose$Dose * dose$Wt, 2), DV = 0,
                     EVID = 1, MDV = 1, CMT = 1, WT = dose$Wt)
obs <- d[d$Time > 0, ]
observations <- data.frame(ID = obs$Subject, TIME = obs$Time, AMT = 0, DV = obs$conc,
                           EVID = 0, MDV = 0, CMT = 2, WT = obs$Wt)
pk <- rbind(dosing, observations)
pk <- pk[order(pk$ID, pk$TIME, -pk$EVID), ]
write.csv(pk, "data/pk.csv", row.names = FALSE, quote = FALSE)
R

cat > R/02_qc.R <<'R'
# Datenqualität
pk <- read.csv("data/pk.csv")
obs <- pk[pk$EVID == 0, ]
checks <- data.frame(
  pruefung = c("fehlende Werte", "negative Konzentrationen", "doppelte Zeitpunkte",
               "Probanden ohne Dosis", "Beobachtungen je Proband (min)"),
  wert = c(sum(is.na(pk)), sum(obs$DV < 0), sum(duplicated(obs[, c("ID", "TIME")])),
           length(setdiff(unique(obs$ID), pk$ID[pk$EVID == 1])), min(table(obs$ID))))
checks$ok <- c(checks$wert[1:4] == 0, checks$wert[5] >= 5)
write.csv(checks, "qc/qc_befunde.csv", row.names = FALSE)
R

cat > R/03_eda.R <<'R'
# Explorative Analyse: Konzentrations-Zeit-Verläufe und NCA
pk <- read.csv("data/pk.csv")
obs <- pk[pk$EVID == 0, ]
png("figures/spaghetti.png", width = 900, height = 550)
plot(DV ~ TIME, data = obs, type = "n", log = "y", xlab = "Zeit (h)", ylab = "Konzentration (mg/L)",
     main = "Theophyllin: Konzentration über Zeit")
for (i in unique(obs$ID)) lines(DV ~ TIME, data = obs[obs$ID == i, ], col = i)
dev.off()
nca <- do.call(rbind, lapply(split(obs, obs$ID), function(s) {
  s <- s[order(s$TIME), ]
  auc <- sum(diff(s$TIME) * (head(s$DV, -1) + tail(s$DV, -1)) / 2)
  data.frame(ID = s$ID[1], CMAX = max(s$DV), TMAX = s$TIME[which.max(s$DV)], AUC = auc)
}))
nca[, -1] <- signif(nca[, -1], 4)
write.csv(nca, "tables/nca.csv", row.names = FALSE)
R

cat > models/run1.mod <<'NM'
$PROBLEM Theophyllin, 1-Kompartiment, Absorption 1. Ordnung
$INPUT ID TIME AMT DV EVID MDV CMT WT
$DATA ../data/pk.csv IGNORE=@
$SUBROUTINES ADVAN2 TRANS2
$PK
  KA = THETA(1)*EXP(ETA(1))
  CL = THETA(2)*EXP(ETA(2))
  V  = THETA(3)*EXP(ETA(3))
  S2 = V
$ERROR
  IPRED = F
  W = SQRT(THETA(4)**2 + (THETA(5)*IPRED)**2)
  IRES = DV - IPRED
  IWRES = IRES/W
  Y = IPRED + W*EPS(1)
$THETA (0, 1.5) (0, 2.8) (0, 32) (0, 0.3) (0, 0.1)
$OMEGA 0.4 0.1 0.05
$SIGMA 1 FIX
$ESTIMATION METHOD=1 INTER MAXEVAL=9999 PRINT=5 NOABORT
$COVARIANCE
$TABLE ID TIME DV PRED IPRED CWRES IWRES NOPRINT ONEHEADER NOAPPEND FILE=sdtab1
NM
sed -e 's/^\$PROBLEM .*/$PROBLEM Theophyllin, 1-Kompartiment, Gewicht allometrisch auf CL und V/' \
    -e 's/^  CL = THETA(2)\*EXP(ETA(2))/  CL = THETA(2)*(WT\/70)**0.75*EXP(ETA(2))/' \
    -e 's/^  V  = THETA(3)\*EXP(ETA(3))/  V  = THETA(3)*(WT\/70)*EXP(ETA(3))/' \
    -e 's/FILE=sdtab1/FILE=sdtab2/' models/run1.mod > models/run2.mod

cat > R/04_modelle.R <<'R'
# Modellvergleich aus den .ext-Dateien
ext <- function(run) {
  x <- read.table(sprintf("models/%s.ext", run), skip = 1, header = TRUE)
  est <- x[x$ITERATION == -1000000000, ]
  data.frame(modell = run, OFV = est$OBJ, KA = est$THETA1, CL = est$THETA2, V = est$THETA3)
}
m <- rbind(ext("run1"), ext("run2"))
m$dOFV <- m$OFV - m$OFV[1]
m[, -1] <- signif(m[, -1], 4)
write.csv(m, "tables/modelle.csv", row.names = FALSE)
R

cat > R/05_gof.R <<'R'
# Goodness of fit des finalen Modells (run2)
sd <- read.table("models/sdtab2", skip = 1, header = TRUE)
sd <- sd[sd$DV > 0, ]
png("figures/gof_run2.png", width = 1000, height = 500)
par(mfrow = c(1, 2))
plot(DV ~ IPRED, data = sd, xlab = "individuelle Vorhersage", ylab = "beobachtet", main = "DV gegen IPRED")
abline(0, 1, col = "grey40")
plot(CWRES ~ TIME, data = sd, xlab = "Zeit (h)", ylab = "CWRES", main = "CWRES gegen Zeit")
abline(h = 0, col = "grey40")
dev.off()
R

cat > report/bericht.qmd <<'Q'
---
title: "Theophyllin: populationspharmakokinetische Analyse"
format:
  html:
    embed-resources: true
execute:
  echo: false
---

## Daten und Qualität

```{r}
knitr::kable(read.csv("../qc/qc_befunde.csv"))
```

## Explorative Analyse

![](../figures/spaghetti.png)

```{r}
knitr::kable(read.csv("../tables/nca.csv"), caption = "NCA je Proband")
```

## Modelle

```{r}
knitr::kable(read.csv("../tables/modelle.csv"), caption = "run1: Basismodell, run2: mit Gewicht")
```

![](../figures/gof_run2.png)
Q

# ---------------------------------------------------------------- the recorded session
LOG="$TRACE/shell-$(date +%Y%m%d-%H%M%S).tsv"
mkdir -p "$TRACE"
N=0
step() { # $1: label, $2: the command as typed
  N=$((N + 1))
  say "$N. $1:  $2"
  ST=ok
  env LD_PRELOAD="$SHIM" KTON_TRACE_LOG="$LOG" KTON_TRACE_HASH="$PROJ" PATH="$PATH:$QUARTO_BIN" sh -c "$2" || ST=err
  # the step's boundary, written from outside like the Claude Code hook does
  printf '%s\t%d\t0\tTASK\t%s\t%d\t0\t0\t0\t%s\n' "$(date +%s%N)" "$$" "$ST" "$N" "$2" >> "$LOG"
}

step "Daten"              "Rscript R/01_daten.R"
step "Datenqualität"      "Rscript R/02_qc.R"
step "Explorative Analyse" "Rscript R/03_eda.R"
step "Basismodell"        "cd models && nmfe75 run1.mod run1.lst > run1.log 2>&1"
step "Modell mit Gewicht" "cd models && nmfe75 run2.mod run2.lst > run2.log 2>&1"
step "Modellvergleich"    "Rscript R/04_modelle.R"
step "Goodness of fit"    "Rscript R/05_gof.R"
step "Bericht"            "quarto render report/bericht.qmd --quiet"

say "fertig: $LOG"
cat tables/modelle.csv
ls report

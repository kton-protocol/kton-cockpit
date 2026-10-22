"""
Stands in for an interactive R session (R is not installable in the build
environment). One long-lived process, steps run one after another like
console commands; after each step a TASK marker is appended to the trace log,
exactly what the R task callback in r/kton-trace.R does.

Each step uses a different way of touching files, chosen to mirror R:
  csv module               ~ read.csv / readr
  ctypes libc open/read    ~ data.table::fread / vroom (C code inside the session)
  subprocess shell         ~ system("...")  (e.g. NONMEM via nmfe)
  multiprocessing fork     ~ parallel::mclapply
  socket connect           ~ url(), DB connection
  temp file + rename       ~ atomic writers (saveRDS via tempfile, arrow, ...)
"""
import csv, ctypes, os, socket, subprocess, sys, time
import multiprocessing as mp

LOG = os.environ["KTON_TRACE_LOG"]
n = 0

def mark(label, status="ok"):
    global n
    n += 1
    line = f"{time.time_ns()}\t{os.getpid()}\t{os.getppid()}\tTASK\t{status}\t{n}\t0\t0\t0\t{label}\n"
    fd = os.open(LOG, os.O_WRONLY | os.O_APPEND)
    os.write(fd, line.encode()); os.close(fd)

def read_csv(p):
    with open(p, newline="") as f:
        return list(csv.DictReader(f))

def write_csv(rows, p):
    with open(p, "w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0]))
        w.writeheader()
        w.writerows(rows)

def merge(a, b, on):
    idx = {r[on]: r for r in b}
    return [{**r, **idx[r[on]]} for r in a if r[on] in idx]

def read_sub(p):
    return len(read_csv(p))

def c_read(path):
    libc = ctypes.CDLL(None)
    fd = libc.open(path.encode(), os.O_RDONLY)
    buf = ctypes.create_string_buffer(1 << 16)
    total = 0
    while (k := libc.read(fd, buf, len(buf))) > 0:
        total += k
    libc.close(fd)
    return total

# 1
pk = read_csv("data/raw.csv")
mark("pk <- read.csv('data/raw.csv')")

# 2
clean = [r for r in pk if float(r["conc"]) > 0]
write_csv(clean, "derived/clean.csv")
mark("clean <- subset(pk, conc > 0); write.csv(clean, 'derived/clean.csv')")

# 3
nbytes = c_read("data/dose.csv")
mark("dose <- data.table::fread('data/dose.csv')   # C-Leser im Prozess")

# 4
subprocess.run("wc -l data/dose.csv > derived/n_dose.txt", shell=True, check=True)
mark("system('wc -l data/dose.csv > derived/n_dose.txt')")

# 5
ctx = mp.get_context("fork")
with ctx.Pool(3) as pool:
    sizes = pool.map(read_sub, [f"data/sub_{i}.csv" for i in (1, 2, 3)])
mark("subs <- mclapply(sprintf('data/sub_%d.csv', 1:3), read.csv)")

# 6
cov = read_csv(os.path.join(os.environ.get("KTON_DEMO_OUTSIDE", os.path.expanduser("~/kton-demo-outside")), "covariates.csv"))
mark("cov <- read.csv('~/kton-demo-outside/covariates.csv')")

# 7  192.0.2.1 is TEST-NET-1 (RFC 5737): never routed, so no real traffic leaves
try:
    s = socket.create_connection(("192.0.2.1", 443), timeout=1)
    s.close()
except OSError:
    pass
mark("lab <- dbGetQuery(con, 'SELECT * FROM lab')   # Live-Verbindung")

# 8
mi = merge(read_csv("derived/clean.csv"), cov, on="id")
write_csv(mi, "derived/.model_input.tmp")
os.rename("derived/.model_input.tmp", "derived/model_input.csv")
mark("mi <- merge(read.csv('derived/clean.csv'), cov); saveRDS(mi, 'derived/model_input.rds')")

# 9
clean2 = [r for r in pk if float(r["conc"]) > 1]
write_csv(clean2, "derived/clean.csv")
files = sorted(os.listdir("data"))
mark("clean <- subset(pk, conc > 1); write.csv(clean, 'derived/clean.csv'); list.files('data')")

with open(LOG, "a") as f:
    f.write(f"{time.time_ns()}\t{os.getpid()}\t0\tEND\t-\t-\t0\t0\t0\t-\n")

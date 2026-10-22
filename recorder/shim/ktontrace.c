/*
 * ktontrace — file/network access recorder (LD_PRELOAD shim).
 *
 * Records, for every process started with this library preloaded, one line per event,
 * appended with a single write() to $KTON_TRACE_LOG (O_APPEND, so lines from forked and
 * exec'd children do not interleave). Nothing is interpreted here; ktonwatch does that.
 *
 * Line format (tab separated; the process that creates the log first writes
 * "#ktontrace v3<TAB>hash=<$KTON_TRACE_HASH>"):
 *   ts_ns pid ppid op mode path size mtime_ns ino extra
 * v2: in path and extra, a backslash, TAB, LF and CR are written as \\, \t, \n, \r (v1: a space).
 * v3: exec and spawn quote an argument with other than safe characters ('run 1.mod') and say so with
 *     mode q; a line without it (an older shim, another writer) is joined by spaces.
 *
 *   op        mode      path            extra
 *   exec      q         cwd             command line              (once per process image)
 *   open      r|w|rw[d] file or dir     k=v;… (sha256, new, trunc, unhashed)
 *   inherit   w|rw      file            k=v;…  a write fd this process received at exec (e.g. `> f`)
 *   openfail  r|w|rw    file            errno=N  (only under $KTON_TRACE_HASH)
 *   closew    w         file            sha256=… (content after the last write through this fd)
 *   rename    -         new path        old path
 *   unlink    -         file            -
 *   connect   net       address         class=loopback|unix|remote;rc=…;errno=…
 *   spawn     q         program         argv (execve/execv/execvp/posix_spawn; child=PID for spawn)
 *
 * While a file PAUSED exists next to the log, nothing is logged (checked at most once a second).
 *
 * Files under $KTON_TRACE_HASH (':'-separated prefixes) are hashed in-process with SHA-256:
 * when opened for reading and when the last tracked write fd is closed. $KTON_TRACE_HASH_MAX
 * (bytes, default 2 GiB) limits that; larger files are logged as unhashed=large.
 *
 * Build:  gcc -shared -fPIC -O2 -o libktontrace.so ktontrace.c -ldl
 * Use:    KTON_TRACE_LOG=/path/trace.tsv KTON_TRACE_HASH=$PWD LD_PRELOAD=/path/libktontrace.so R
 *         KTON_TRACE_LOG=/dir/r-%p.tsv  one log per started session (%p: pid of the first process)
 */
#define _GNU_SOURCE
#include <arpa/inet.h>
#include <dirent.h>
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <netinet/in.h>
#include <spawn.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <sys/un.h>
#include <time.h>
#include <unistd.h>

static int log_fd = -1;
static dev_t log_dev;
static ino_t log_ino;
static __thread int in_hook = 0;
static long long hash_max = 2147483648LL;
/* <log dir>/PAUSED: while it exists nothing is logged (switched by the Positron extension) */
static char pause_path[4200];
static long long pause_checked;
static int paused;

#define MAXFD 65536
/* fds opened for writing on a hashed file; 1 = hash at last close. Atomic: R libraries open
 * and close from several threads. */
static unsigned char wtrack[MAXFD];
static int wt_get(int fd) { return fd >= 0 && fd < MAXFD && __atomic_load_n(&wtrack[fd], __ATOMIC_RELAXED); }
static void wt_set(int fd, unsigned char v) { if (fd >= 0 && fd < MAXFD) __atomic_store_n(&wtrack[fd], v, __ATOMIC_RELAXED); }
static int wt_take(int fd) { return fd >= 0 && fd < MAXFD && __atomic_exchange_n(&wtrack[fd], 0, __ATOMIC_RELAXED); }

/* ------------------------------------------------------------------ */
/* low level helpers (no libc calls that could re-enter our hooks)     */

static long long now_ns(void) {
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    return (long long)ts.tv_sec * 1000000000LL + ts.tv_nsec;
}

static void raw_write(const char *buf, size_t n) {
    if (log_fd < 0) return;
    int saved = errno;
    syscall(SYS_write, log_fd, buf, n);
    errno = saved;
}

/* escape backslash, tab, newline and CR so a line stays a line and the path stays the path */
static void escape_to(char *out, size_t n, const char *s) {
    size_t o = 0;
    for (; *s && o + 2 < n; s++) {
        char c = *s;
        const char *esc = c == '\\' ? "\\\\" : c == '\t' ? "\\t" : c == '\n' ? "\\n" : c == '\r' ? "\\r" : NULL;
        if (esc) { out[o++] = esc[0]; out[o++] = esc[1]; }
        else out[o++] = c;
    }
    out[o] = 0;
}

static int is_paused(void) {
    if (!pause_path[0]) return 0;
    long long now = now_ns();
    if (now - pause_checked > 1000000000LL) { /* at most once a second: a stat, nothing more */
        struct stat st;
        pause_checked = now;
        paused = syscall(SYS_newfstatat, AT_FDCWD, pause_path, &st, 0) == 0;
    }
    return paused;
}

static void emit_line(const char *op, const char *mode, const char *path,
                      long long size, long long mtime, unsigned long long ino,
                      const char *extra) {
    if (is_paused()) return;
    char line[8192];
    char p[4096], e[2048];
    escape_to(p, sizeof p, path ? path : "-");
    escape_to(e, sizeof e, extra && *extra ? extra : "-");
    int n = snprintf(line, sizeof line, "%lld\t%d\t%d\t%s\t%s\t%s\t%lld\t%lld\t%llu\t%s\n",
                     now_ns(), (int)getpid(), (int)getppid(), op, mode, p, size,
                     mtime, ino, e);
    if (n > 0) raw_write(line, (size_t)n < sizeof line ? (size_t)n : sizeof line - 1);
}

static long long mtime_of(const struct stat *st) {
    return (long long)st->st_mtim.tv_sec * 1000000000LL + st->st_mtim.tv_nsec;
}

/* append "k=v" to a ';'-separated field list */
static void add_kv(char *buf, size_t n, const char *kv) {
    size_t l = strlen(buf);
    if (l && l + 1 < n) { buf[l++] = ';'; buf[l] = 0; }
    snprintf(buf + l, n - l, "%s", kv);
}

/* append one argument of a command line, shell-quoted unless it is made of safe characters
 * only, so a reader can split the line again ('run 1.mod' stays one argument) */
static void append_arg(char *buf, size_t n, size_t *l, const char *a, int first) {
    static const char safe[] = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_./=:,+@%";
    if (*l + 2 >= n) return;
    if (!first) buf[(*l)++] = ' ';
    int plain = *a != 0;
    for (const char *p = a; *p; p++) if (!strchr(safe, *p)) { plain = 0; break; }
    if (plain) { *l += (size_t)snprintf(buf + *l, n - *l, "%s", a); if (*l >= n) *l = n - 1; return; }
    buf[(*l)++] = '\'';
    for (const char *p = a; *p && *l + 5 < n; p++) {
        if (*p == '\'') { memcpy(buf + *l, "'\\''", 4); *l += 4; }
        else buf[(*l)++] = *p;
    }
    if (*l + 1 < n) buf[(*l)++] = '\'';
    buf[*l] = 0;
}

/* ------------------------------------------------------------------ */
/* SHA-256 (compact, public domain style)                              */
typedef struct { unsigned int h[8]; unsigned long long len; unsigned char buf[64]; unsigned int n; } sha256_ctx;
static const unsigned int K256[64] = {
0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2};
#define ROR(x,n) (((x)>>(n))|((x)<<(32-(n))))
static void sha256_block(sha256_ctx *c, const unsigned char *p) {
    unsigned int w[64], a,b,d,e,f,g,h,cc,t1,t2;
    for (int i=0;i<16;i++) w[i]=(p[4*i]<<24)|(p[4*i+1]<<16)|(p[4*i+2]<<8)|p[4*i+3];
    for (int i=16;i<64;i++){unsigned int s0=ROR(w[i-15],7)^ROR(w[i-15],18)^(w[i-15]>>3),s1=ROR(w[i-2],17)^ROR(w[i-2],19)^(w[i-2]>>10);w[i]=w[i-16]+s0+w[i-7]+s1;}
    a=c->h[0];b=c->h[1];cc=c->h[2];d=c->h[3];e=c->h[4];f=c->h[5];g=c->h[6];h=c->h[7];
    for (int i=0;i<64;i++){t1=h+(ROR(e,6)^ROR(e,11)^ROR(e,25))+((e&f)^(~e&g))+K256[i]+w[i];t2=(ROR(a,2)^ROR(a,13)^ROR(a,22))+((a&b)^(a&cc)^(b&cc));h=g;g=f;f=e;e=d+t1;d=cc;cc=b;b=a;a=t1+t2;}
    c->h[0]+=a;c->h[1]+=b;c->h[2]+=cc;c->h[3]+=d;c->h[4]+=e;c->h[5]+=f;c->h[6]+=g;c->h[7]+=h;
}
static void sha256_init(sha256_ctx *c){static const unsigned int iv[8]={0x6a09e667,0xbb67ae85,0x3c6ef372,0xa54ff53a,0x510e527f,0x9b05688c,0x1f83d9ab,0x5be0cd19};memcpy(c->h,iv,sizeof iv);c->len=0;c->n=0;}
static void sha256_update(sha256_ctx *c,const unsigned char *p,size_t n){c->len+=n;while(n){size_t k=64-c->n;if(k>n)k=n;memcpy(c->buf+c->n,p,k);c->n+=k;p+=k;n-=k;if(c->n==64){sha256_block(c,c->buf);c->n=0;}}}
static void sha256_hex(sha256_ctx *c,char out[65]){unsigned long long bits=c->len*8;unsigned char pad=0x80,z=0;sha256_update(c,&pad,1);while(c->n!=56)sha256_update(c,&z,1);unsigned char L[8];for(int i=0;i<8;i++)L[i]=bits>>(56-8*i);sha256_update(c,L,8);for(int i=0;i<8;i++)sprintf(out+8*i,"%08x",c->h[i]);out[64]=0;}

/* hash an open fd with pread: same inode the process sees, file offset untouched */
static int hash_raw(int fd, char out[65]) {
    sha256_ctx c; sha256_init(&c);
    static __thread unsigned char buf[1 << 16];
    off_t off = 0; ssize_t k;
    while ((k = (ssize_t)syscall(SYS_pread64, fd, buf, sizeof buf, off)) > 0) { sha256_update(&c, buf, (size_t)k); off += k; }
    if (k < 0) return -1;
    sha256_hex(&c, out);
    return 0;
}

/* Per-process cache keyed by (dev, ino, size, mtime): the same big input opened again is not
 * hashed again. A changed file has a new size or mtime and misses, except a same-size rewrite
 * within one tick of the coarse clock the kernel stamps mtime from. So: a file whose mtime is
 * not at least HC_RACY_NS old when it is hashed is not cached (as git does with racy entries),
 * opening a write fd drops the inode's slot, and the hash at close never comes from the cache.
 * The lock is only ever tried: a child forked while another thread held it (mclapply) must not
 * spin forever, so whoever does not get it goes without the cache. */
#define HC 256
#define HC_RACY_NS 2000000000LL
static struct { dev_t dev; ino_t ino; long long size, mtime; char hex[65]; } hcache[HC];
static int hc_lock;
static int hc_try(void) { return !__atomic_exchange_n(&hc_lock, 1, __ATOMIC_ACQUIRE); }
static void hc_release(void) { __atomic_store_n(&hc_lock, 0, __ATOMIC_RELEASE); }
static unsigned hc_slot(const struct stat *st) { return (unsigned)((st->st_ino * 2654435761u) ^ st->st_dev) % HC; }

static void hc_drop(const struct stat *st) {
    unsigned slot = hc_slot(st);
    if (!hc_try()) return;
    if (hcache[slot].dev == st->st_dev && hcache[slot].ino == st->st_ino) hcache[slot].hex[0] = 0;
    hc_release();
}

/* hash an fd; falls back to reopening /proc/self/fd/N when the fd is write-only (pread: EBADF).
 * use_cache 0: hash the bytes now (the content at close). */
static int hash_fd(int fd, const struct stat *st, int use_cache, char out[65]) {
    unsigned slot = hc_slot(st);
    if (use_cache && hc_try()) {
        int hit = hcache[slot].hex[0] && hcache[slot].dev == st->st_dev && hcache[slot].ino == st->st_ino &&
                  hcache[slot].size == (long long)st->st_size && hcache[slot].mtime == mtime_of(st);
        if (hit) memcpy(out, hcache[slot].hex, 65);
        hc_release();
        if (hit) return 0;
    }
    int rc = hash_raw(fd, out);
    if (rc != 0) {
        char link[64];
        snprintf(link, sizeof link, "/proc/self/fd/%d", fd);
        int rfd = (int)syscall(SYS_openat, AT_FDCWD, link, O_RDONLY | O_CLOEXEC);
        if (rfd < 0) return -1;
        rc = hash_raw(rfd, out);
        syscall(SYS_close, rfd);
        if (rc != 0) return -1;
    }
    if (now_ns() - mtime_of(st) >= HC_RACY_NS && hc_try()) {
        hcache[slot].dev = st->st_dev; hcache[slot].ino = st->st_ino;
        hcache[slot].size = (long long)st->st_size; hcache[slot].mtime = mtime_of(st);
        memcpy(hcache[slot].hex, out, 65);
        hc_release();
    }
    return 0;
}

/* only files under these prefixes are hashed in-process ($KTON_TRACE_HASH, ':' separated) */
static char hash_prefixes[4096];
static int under_hash_prefix(const char *p) {
    if (!hash_prefixes[0]) return 0;
    const char *s = hash_prefixes;
    while (*s) {
        const char *e = strchr(s, ':'); size_t n = e ? (size_t)(e - s) : strlen(s);
        if (n && strncmp(p, s, n) == 0 && (p[n] == '/' || p[n] == 0)) return 1;
        if (!e) break;
        s = e + 1;
    }
    return 0;
}

/* "sha256=…" or "unhashed=large" into extra */
static void add_hash(int fd, const struct stat *st, int use_cache, char *extra, size_t n) {
    char h[80];
    if ((long long)st->st_size > hash_max) { add_kv(extra, n, "unhashed=large"); return; }
    char hex[65];
    if (hash_fd(fd, st, use_cache, hex) == 0) { snprintf(h, sizeof h, "sha256=%s", hex); add_kv(extra, n, h); }
}

static int fd_path(int fd, char *path, size_t n) {
    char link[64];
    snprintf(link, sizeof link, "/proc/self/fd/%d", fd);
    ssize_t len = readlink(link, path, n - 1);
    if (len <= 0) return -1;
    path[len] = 0;
    return 0;
}

/* absolute path for (dirfd, p) without resolving the last component */
static void abs_path(int dirfd, const char *p, char *out, size_t n) {
    if (!p) { snprintf(out, n, "-"); return; }
    if (p[0] == '/') { snprintf(out, n, "%s", p); return; }
    char base[4096] = "";
    if (dirfd == AT_FDCWD) {
        if (!getcwd(base, sizeof base)) base[0] = 0;
    } else if (fd_path(dirfd, base, sizeof base) != 0) {
        base[0] = 0;
    }
    snprintf(out, n, "%s/%s", base, p);
}

/* canonical path: the directory resolved (symlinks, ..), the last component kept as given,
 * so it matches what /proc/self/fd shows for an open file and still works after unlink */
static void canon_path(int dirfd, const char *p, char *out, size_t n) {
    char a[4096];
    abs_path(dirfd, p, a, sizeof a);
    char *slash = strrchr(a, '/');
    if (!slash) { snprintf(out, n, "%s", a); return; }
    char dir[4096], res[PATH_MAX];
    size_t dl = (size_t)(slash - a);
    if (dl == 0) { snprintf(dir, sizeof dir, "/"); } else { memcpy(dir, a, dl); dir[dl] = 0; }
    if (realpath(dir, res)) snprintf(out, n, "%s/%s", strcmp(res, "/") ? res : "", slash + 1);
    else snprintf(out, n, "%s", a);
}

/* ------------------------------------------------------------------ */
/* events                                                              */

/* record an fd we just opened: canonical path via /proc */
static void emit_fd(const char *op, const char *mode, int fd, int is_new, int trunc) {
    if (fd < 0 || log_fd < 0) return;
    struct stat st;
    if (fstat(fd, &st) != 0) return;
    if (st.st_dev == log_dev && st.st_ino == log_ino) return; /* our own log */
    if (!S_ISREG(st.st_mode) && !S_ISDIR(st.st_mode)) return;  /* pipes, ttys, sockets */
    char path[4096];
    if (fd_path(fd, path, sizeof path) != 0) return;
    char m[8], extra[256] = "";
    snprintf(m, sizeof m, "%s%s", mode, S_ISDIR(st.st_mode) ? "d" : "");
    if (is_new) add_kv(extra, sizeof extra, "new=1");
    if (trunc) add_kv(extra, sizeof extra, "trunc=1");
    if (S_ISREG(st.st_mode) && under_hash_prefix(path)) {
        if (mode[0] == 'r' && !is_new && !trunc) add_hash(fd, &st, 1, extra, sizeof extra);
        if (strchr(mode, 'w')) { wt_set(fd, 1); hc_drop(&st); }
    }
    emit_line(op, m, path, (long long)st.st_size, mtime_of(&st), (unsigned long long)st.st_ino, extra);
}

/* final content of a written file, taken right before its fd is closed */
static void emit_close(int fd) {
    if (!wt_take(fd) || log_fd < 0) return;
    struct stat st; char path[4096], extra[128] = "";
    if (fstat(fd, &st) != 0 || !S_ISREG(st.st_mode)) return;
    if (fd_path(fd, path, sizeof path) != 0) return;
    add_hash(fd, &st, 0, extra, sizeof extra);
    if (!extra[0]) return;
    emit_line("closew", "w", path, (long long)st.st_size, mtime_of(&st), (unsigned long long)st.st_ino, extra);
}

/* a failed open of a file that would have been hashed: a tried input that was not there */
static void emit_openfail(int dirfd, const char *p, const char *mode, int err) {
    if (log_fd < 0 || !p) return;
    char c[4096];
    canon_path(dirfd, p, c, sizeof c);
    if (!under_hash_prefix(c)) return;
    char extra[32];
    snprintf(extra, sizeof extra, "errno=%d", err);
    emit_line("openfail", mode, c, 0, 0, 0, extra);
}

static const char *flags_mode(int flags) {
    switch (flags & O_ACCMODE) {
    case O_RDONLY: return "r";
    case O_WRONLY: return "w";
    default: return "rw";
    }
}

static const char *fopen_mode(const char *m) {
    if (!m) return "r";
    int plus = strchr(m, '+') != NULL;
    if (m[0] == 'r') return plus ? "rw" : "r";
    return plus ? "rw" : "w"; /* w, a, x */
}

/* did (dirfd, path) exist before an O_CREAT open? */
static int exists_at(int dirfd, const char *p) {
    struct stat st;
    return p && syscall(SYS_newfstatat, dirfd, p, &st, 0) == 0;
}

/* ------------------------------------------------------------------ */
/* setup: open log, record the exec of this process image              */

__attribute__((constructor)) static void kton_init(void) {
    const char *p = getenv("KTON_TRACE_LOG");
    if (!p || !*p) return;
    /* "%p" becomes this process's pid, once: the expanded path is put back into the
     * environment so children log into the same file (one log per kernel, not per process) */
    char expanded[4096];
    const char *pp = strstr(p, "%p");
    if (pp) {
        snprintf(expanded, sizeof expanded, "%.*s%d%s", (int)(pp - p), p, (int)getpid(), pp + 2);
        setenv("KTON_TRACE_LOG", expanded, 1);
        p = expanded;
    }
    log_fd = (int)syscall(SYS_openat, AT_FDCWD, p,
                          O_WRONLY | O_APPEND | O_CREAT | O_CLOEXEC, 0644);
    if (log_fd < 0) return;
    {
        const char *slash = strrchr(p, '/');
        if (slash) snprintf(pause_path, sizeof pause_path, "%.*s/PAUSED", (int)(slash - p), p);
    }
    struct stat st;
    const char *hp = getenv("KTON_TRACE_HASH");
    if (hp) snprintf(hash_prefixes, sizeof hash_prefixes, "%s", hp);
    if (fstat(log_fd, &st) == 0) {
        log_dev = st.st_dev; log_ino = st.st_ino;
        if (st.st_size == 0) {
            char hdr[4200];
            int n = snprintf(hdr, sizeof hdr, "#ktontrace v3\thash=%s\n", hash_prefixes);
            if (n > 0) raw_write(hdr, (size_t)n < sizeof hdr ? (size_t)n : sizeof hdr - 1);
        }
    }
    const char *hm = getenv("KTON_TRACE_HASH_MAX");
    if (hm && *hm) hash_max = atoll(hm);

    char raw[2048] = {0}, cmd[2048] = {0}, cwd[4096] = "-";
    int fd = (int)syscall(SYS_openat, AT_FDCWD, "/proc/self/cmdline", O_RDONLY | O_CLOEXEC);
    if (fd >= 0) {
        ssize_t n = syscall(SYS_read, fd, raw, sizeof raw - 1);
        syscall(SYS_close, fd);
        size_t l = 0;
        for (ssize_t i = 0; i < n; i += (ssize_t)strlen(raw + i) + 1) append_arg(cmd, sizeof cmd, &l, raw + i, i == 0);
    }
    if (!getcwd(cwd, sizeof cwd)) strcpy(cwd, "-");
    in_hook = 1;
    emit_line("exec", "q", cwd, 0, 0, 0, cmd);

    /* Write fds this image received from its parent (a shell's `> file`): the parent closed its
     * copy before anything was written, so only this process can report the final content. */
    for (int i = 0; i < 1024; i++) {
        if (i == log_fd) continue;
        long fl = syscall(SYS_fcntl, i, F_GETFL);
        if (fl < 0 || (fl & O_ACCMODE) == O_RDONLY) continue;
        struct stat fst;
        if (fstat(i, &fst) != 0 || !S_ISREG(fst.st_mode)) continue;
        char path[4096];
        if (fd_path(i, path, sizeof path) != 0 || !under_hash_prefix(path)) continue;
        wt_set(i, 1);
        hc_drop(&fst);
        emit_line("inherit", (fl & O_ACCMODE) == O_WRONLY ? "w" : "rw", path, (long long)fst.st_size,
                  mtime_of(&fst), (unsigned long long)fst.st_ino, "-");
    }
    in_hook = 0;
}

/* at exit: report what is still open for writing (stdout redirected to a file is never closed) */
__attribute__((destructor)) static void kton_fini(void) {
    if (log_fd < 0) return;
    in_hook = 1;
    for (int i = 0; i < MAXFD; i++) if (wt_get(i)) emit_close(i);
    in_hook = 0;
}

/* ------------------------------------------------------------------ */
/* interposed functions                                                */

#define REAL(name) \
    static __typeof__(name) *real_##name; \
    if (!real_##name) real_##name = dlsym(RTLD_NEXT, #name)

#define TAKE_MODE(flags) \
    mode_t mode = 0; \
    if ((flags) & O_CREAT || ((flags) & O_TMPFILE) == O_TMPFILE) { \
        va_list ap; va_start(ap, flags); mode = va_arg(ap, int); va_end(ap); }

static int after_open(int fd, int dirfd, const char *path, int flags, int existed, int err) {
    if (in_hook) return fd;
    in_hook = 1;
    if (fd >= 0) emit_fd("open", flags_mode(flags), fd, (flags & O_CREAT) && !existed, (flags & O_TRUNC) && existed);
    else if (err == ENOENT) emit_openfail(dirfd, path, flags_mode(flags), err);
    in_hook = 0;
    errno = err;
    return fd;
}

#define OPEN_BODY(dirfd, callexpr) \
    int existed = ((flags & O_CREAT) && log_fd >= 0 && !in_hook) ? exists_at(dirfd, path) : 1; \
    int fd = callexpr; \
    return after_open(fd, dirfd, path, flags, existed, errno);

int open(const char *path, int flags, ...) {
    TAKE_MODE(flags); REAL(open);
    OPEN_BODY(AT_FDCWD, real_open(path, flags, mode));
}
int open64(const char *path, int flags, ...) {
    TAKE_MODE(flags); REAL(open64);
    OPEN_BODY(AT_FDCWD, real_open64(path, flags, mode));
}
int openat(int dirfd, const char *path, int flags, ...) {
    TAKE_MODE(flags); REAL(openat);
    OPEN_BODY(dirfd, real_openat(dirfd, path, flags, mode));
}
int openat64(int dirfd, const char *path, int flags, ...) {
    TAKE_MODE(flags); REAL(openat64);
    OPEN_BODY(dirfd, real_openat64(dirfd, path, flags, mode));
}
/* _FORTIFY_SOURCE variants */
int __open_2(const char *path, int flags) {
    REAL(__open_2); OPEN_BODY(AT_FDCWD, real___open_2(path, flags));
}
int __open64_2(const char *path, int flags) {
    REAL(__open64_2); OPEN_BODY(AT_FDCWD, real___open64_2(path, flags));
}
int __openat_2(int dirfd, const char *path, int flags) {
    REAL(__openat_2); OPEN_BODY(dirfd, real___openat_2(dirfd, path, flags));
}
int __openat64_2(int dirfd, const char *path, int flags) {
    REAL(__openat64_2); OPEN_BODY(dirfd, real___openat64_2(dirfd, path, flags));
}
int creat(const char *path, mode_t mode) {
    int flags = O_WRONLY | O_CREAT | O_TRUNC;
    REAL(creat); OPEN_BODY(AT_FDCWD, real_creat(path, mode));
}
int creat64(const char *path, mode_t mode) {
    int flags = O_WRONLY | O_CREAT | O_TRUNC;
    REAL(creat64); OPEN_BODY(AT_FDCWD, real_creat64(path, mode));
}

static int fopen_flags(const char *m) {
    if (!m) return O_RDONLY;
    int plus = strchr(m, '+') != NULL;
    if (m[0] == 'r') return plus ? O_RDWR : O_RDONLY;
    int f = (plus ? O_RDWR : O_WRONLY) | O_CREAT;
    if (m[0] == 'w') f |= O_TRUNC;
    return f;
}

static FILE *after_fopen(FILE *f, const char *path, const char *m, int existed, int err) {
    if (in_hook) return f;
    in_hook = 1;
    int flags = fopen_flags(m);
    if (f) emit_fd("open", fopen_mode(m), fileno(f), (flags & O_CREAT) && !existed, (flags & O_TRUNC) && existed);
    else if (err == ENOENT) emit_openfail(AT_FDCWD, path, fopen_mode(m), err);
    in_hook = 0;
    errno = err;
    return f;
}

#define FOPEN_BODY(callexpr) \
    int existed = ((fopen_flags(m) & O_CREAT) && log_fd >= 0 && !in_hook) ? exists_at(AT_FDCWD, path) : 1; \
    FILE *f = callexpr; \
    return after_fopen(f, path, m, existed, errno);

FILE *fopen(const char *path, const char *m) {
    REAL(fopen); FOPEN_BODY(real_fopen(path, m));
}
FILE *fopen64(const char *path, const char *m) {
    REAL(fopen64); FOPEN_BODY(real_fopen64(path, m));
}
FILE *freopen(const char *path, const char *m, FILE *s) {
    REAL(freopen); FOPEN_BODY(real_freopen(path, m, s));
}
FILE *freopen64(const char *path, const char *m, FILE *s) {
    REAL(freopen64); FOPEN_BODY(real_freopen64(path, m, s));
}

static int do_rename(const char *a, const char *b, int rc) {
    if (rc == 0 && !in_hook && log_fd >= 0) {
        in_hook = 1;
        emit_line("rename", "-", b, 0, 0, 0, a);
        in_hook = 0;
    }
    return rc;
}
/* paths are made canonical before the call: afterwards the source no longer exists */
#define RENAME_BODY(ofd, o, nfd, n, callexpr) \
    char a[4096] = "", b[4096] = ""; int saved; \
    if (log_fd >= 0 && !in_hook) { in_hook = 1; canon_path(ofd, o, a, sizeof a); canon_path(nfd, n, b, sizeof b); in_hook = 0; } \
    int rc = callexpr; saved = errno; \
    if (log_fd >= 0) do_rename(a, b, rc); \
    errno = saved; return rc;

int rename(const char *o, const char *n) {
    REAL(rename); RENAME_BODY(AT_FDCWD, o, AT_FDCWD, n, real_rename(o, n));
}
int renameat(int ofd, const char *o, int nfd, const char *n) {
    REAL(renameat); RENAME_BODY(ofd, o, nfd, n, real_renameat(ofd, o, nfd, n));
}
int renameat2(int ofd, const char *o, int nfd, const char *n, unsigned int flags) {
    REAL(renameat2); RENAME_BODY(ofd, o, nfd, n, real_renameat2(ofd, o, nfd, n, flags));
}

#define UNLINK_BODY(dirfd, p, callexpr) \
    char c[4096]; c[0] = 0; \
    if (log_fd >= 0 && !in_hook) { in_hook = 1; canon_path(dirfd, p, c, sizeof c); in_hook = 0; } \
    int rc = callexpr; int saved = errno; \
    if (rc == 0 && c[0] && !in_hook) { in_hook = 1; emit_line("unlink", "-", c, 0, 0, 0, "-"); in_hook = 0; } \
    errno = saved; return rc;

int unlink(const char *p) {
    REAL(unlink); UNLINK_BODY(AT_FDCWD, p, real_unlink(p));
}
int unlinkat(int dirfd, const char *p, int flags) {
    REAL(unlinkat);
    if (flags & AT_REMOVEDIR) return real_unlinkat(dirfd, p, flags);
    UNLINK_BODY(dirfd, p, real_unlinkat(dirfd, p, flags));
}
int remove(const char *p) {
    REAL(remove);
    struct stat st;
    if (syscall(SYS_newfstatat, AT_FDCWD, p, &st, AT_SYMLINK_NOFOLLOW) == 0 && S_ISDIR(st.st_mode)) return real_remove(p);
    UNLINK_BODY(AT_FDCWD, p, real_remove(p));
}

static const char *net_class(const struct sockaddr *addr) {
    if (addr->sa_family == AF_UNIX) return "unix";
    if (addr->sa_family == AF_INET) {
        const struct sockaddr_in *a = (const void *)addr;
        return (ntohl(a->sin_addr.s_addr) >> 24) == 127 ? "loopback" : "remote";
    }
    if (addr->sa_family == AF_INET6) {
        const struct sockaddr_in6 *a = (const void *)addr;
        if (IN6_IS_ADDR_LOOPBACK(&a->sin6_addr)) return "loopback";
        if (IN6_IS_ADDR_V4MAPPED(&a->sin6_addr) && a->sin6_addr.s6_addr[12] == 127) return "loopback";
        return "remote";
    }
    return "other";
}

int connect(int fd, const struct sockaddr *addr, socklen_t len) {
    REAL(connect);
    int rc = real_connect(fd, addr, len);
    int saved = errno;
    if (!in_hook && log_fd >= 0 && addr) {
        in_hook = 1;
        char where[512] = "?", res[96];
        if (addr->sa_family == AF_INET) {
            const struct sockaddr_in *a = (const void *)addr;
            char ip[INET_ADDRSTRLEN];
            inet_ntop(AF_INET, &a->sin_addr, ip, sizeof ip);
            snprintf(where, sizeof where, "inet:%s:%d", ip, ntohs(a->sin_port));
        } else if (addr->sa_family == AF_INET6) {
            const struct sockaddr_in6 *a = (const void *)addr;
            char ip[INET6_ADDRSTRLEN];
            inet_ntop(AF_INET6, &a->sin6_addr, ip, sizeof ip);
            snprintf(where, sizeof where, "inet6:[%s]:%d", ip, ntohs(a->sin6_port));
        } else if (addr->sa_family == AF_UNIX) {
            const struct sockaddr_un *a = (const void *)addr;
            snprintf(where, sizeof where, "unix:%s", a->sun_path[0] ? a->sun_path : "@abstract");
        }
        snprintf(res, sizeof res, "class=%s;rc=%d;errno=%d", net_class(addr), rc, rc == 0 ? 0 : saved);
        emit_line("connect", "net", where, 0, 0, 0, res);
        in_hook = 0;
    }
    errno = saved;
    return rc;
}

int close(int fd) {
    REAL(close);
    if (!in_hook && wt_get(fd)) { in_hook = 1; emit_close(fd); in_hook = 0; }
    return real_close(fd);
}
int fclose(FILE *f) {
    REAL(fclose);
    int fd = fileno(f);
    if (!in_hook && wt_get(fd)) { in_hook = 1; fflush(f); emit_close(fd); in_hook = 0; }
    return real_fclose(f);
}

/* a duplicate of a tracked write fd is tracked too; the original is hashed at its own close,
 * the last close reports the final content */
static int after_dup(int oldfd, int newfd) {
    if (newfd >= 0 && newfd != oldfd) {
        if (wt_get(newfd) && !in_hook) { in_hook = 1; emit_close(newfd); in_hook = 0; } /* dup2 closes newfd */
        wt_set(newfd, wt_get(oldfd));
    }
    return newfd;
}
int dup(int oldfd) { REAL(dup); int s, r = real_dup(oldfd); s = errno; after_dup(oldfd, r); errno = s; return r; }
int dup2(int oldfd, int newfd) {
    REAL(dup2);
    if (newfd != oldfd && wt_get(newfd) && !in_hook) { in_hook = 1; emit_close(newfd); in_hook = 0; }
    int r = real_dup2(oldfd, newfd), s = errno;
    if (r >= 0 && newfd != oldfd) wt_set(newfd, wt_get(oldfd));
    errno = s; return r;
}
int dup3(int oldfd, int newfd, int flags) {
    REAL(dup3);
    if (newfd != oldfd && wt_get(newfd) && !in_hook) { in_hook = 1; emit_close(newfd); in_hook = 0; }
    int r = real_dup3(oldfd, newfd, flags), s = errno;
    if (r >= 0) wt_set(newfd, wt_get(oldfd));
    errno = s; return r;
}
#define FCNTL_BODY(realcall) \
    va_list ap; va_start(ap, cmd); void *arg = va_arg(ap, void *); va_end(ap); \
    int r = realcall; int s = errno; \
    if (r >= 0 && (cmd == F_DUPFD || cmd == F_DUPFD_CLOEXEC)) after_dup(fd, r); \
    errno = s; return r;
int fcntl(int fd, int cmd, ...) { REAL(fcntl); FCNTL_BODY(real_fcntl(fd, cmd, arg)); }
int fcntl64(int fd, int cmd, ...) { REAL(fcntl64); FCNTL_BODY(real_fcntl64(fd, cmd, arg)); }

DIR *opendir(const char *path) {
    REAL(opendir);
    DIR *d = real_opendir(path);
    if (d && !in_hook) { int s = errno; in_hook = 1; emit_fd("open", "r", dirfd(d), 0, 0); in_hook = 0; errno = s; }
    return d;
}

/* Program starts. A dynamically linked program logs its own `exec` line; a `spawn` with no
 * `exec` following from that pid is a program the shim could not enter (statically linked). */
static void emit_spawn(const char *path, char *const argv[], pid_t child) {
    if (in_hook || log_fd < 0) return;
    in_hook = 1;
    char extra[2048] = "";
    size_t l = 0;
    if (child > 0) l = (size_t)snprintf(extra, sizeof extra, "child=%d;", (int)child);
    if (argv && argv[0]) l += (size_t)snprintf(extra + l, sizeof extra - l, "argv=");
    for (int i = 0; argv && argv[i] && l + 2 < sizeof extra; i++) append_arg(extra, sizeof extra, &l, argv[i], i == 0);
    emit_line("spawn", "q", path, 0, 0, 0, extra);
    in_hook = 0;
}
int execve(const char *path, char *const argv[], char *const envp[]) {
    REAL(execve); emit_spawn(path, argv, 0); return real_execve(path, argv, envp);
}
int execv(const char *path, char *const argv[]) {
    REAL(execv); emit_spawn(path, argv, 0); return real_execv(path, argv);
}
int execvp(const char *file, char *const argv[]) {
    REAL(execvp); emit_spawn(file, argv, 0); return real_execvp(file, argv);
}
int execvpe(const char *file, char *const argv[], char *const envp[]) {
    REAL(execvpe); emit_spawn(file, argv, 0); return real_execvpe(file, argv, envp);
}
int posix_spawn(pid_t *pid, const char *path, const posix_spawn_file_actions_t *fa,
                const posix_spawnattr_t *at, char *const argv[], char *const envp[]) {
    REAL(posix_spawn);
    int rc = real_posix_spawn(pid, path, fa, at, argv, envp);
    if (rc == 0) emit_spawn(path, argv, pid ? *pid : 0);
    return rc;
}
int posix_spawnp(pid_t *pid, const char *file, const posix_spawn_file_actions_t *fa,
                 const posix_spawnattr_t *at, char *const argv[], char *const envp[]) {
    REAL(posix_spawnp);
    int rc = real_posix_spawnp(pid, file, fa, at, argv, envp);
    if (rc == 0) emit_spawn(file, argv, pid ? *pid : 0);
    return rc;
}

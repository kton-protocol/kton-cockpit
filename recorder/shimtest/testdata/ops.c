/* ops: one file/network operation per argument group, for the shim tests.
 *   read F | write F TEXT | writeonly F TEXT | append F TEXT | fopenw F TEXT
 *   rename A B | unlink F | openfail F | connect PORT | dupwrite F TEXT
 *   rewritekeep F TEXT   overwrite F in place from offset 0 and put its old mtime back (touch -r)
 * Runs them in order; exits 0 even if an operation fails (the test reads the log). */
#define _GNU_SOURCE
#include <arpa/inet.h>
#include <fcntl.h>
#include <netinet/in.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <unistd.h>

int main(int argc, char **argv) {
    for (int i = 1; i < argc; i++) {
        const char *op = argv[i];
        if (!strcmp(op, "read")) {
            char b[256]; int fd = open(argv[++i], O_RDONLY); if (fd >= 0) { while (read(fd, b, sizeof b) > 0); close(fd); }
        } else if (!strcmp(op, "write") || !strcmp(op, "writeonly") || !strcmp(op, "append")) {
            int fl = !strcmp(op, "append") ? O_WRONLY | O_CREAT | O_APPEND
                   : !strcmp(op, "write") ? O_RDWR | O_CREAT | O_TRUNC : O_WRONLY | O_CREAT | O_TRUNC;
            const char *f = argv[++i], *t = argv[++i];
            int fd = open(f, fl, 0644); if (fd >= 0) { if (write(fd, t, strlen(t)) < 0) {} close(fd); }
        } else if (!strcmp(op, "fopenw")) {
            const char *f = argv[++i], *t = argv[++i];
            FILE *fp = fopen(f, "w"); if (fp) { fputs(t, fp); fclose(fp); }
        } else if (!strcmp(op, "dupwrite")) {
            const char *f = argv[++i], *t = argv[++i];
            int fd = open(f, O_WRONLY | O_CREAT | O_TRUNC, 0644);
            int d = dup(fd); close(fd);              /* original closed before anything is written */
            if (write(d, t, strlen(t)) < 0) {} close(d);
        } else if (!strcmp(op, "rewritekeep")) {
            const char *f = argv[++i], *t = argv[++i];
            int fd = open(f, O_WRONLY); struct stat st;
            if (fd >= 0 && fstat(fd, &st) == 0) {
                if (pwrite(fd, t, strlen(t), 0) < 0) {}
                struct timespec ts[2] = {st.st_atim, st.st_mtim}; futimens(fd, ts);
                close(fd);
            }
        } else if (!strcmp(op, "rename")) {
            const char *a = argv[++i], *b = argv[++i]; rename(a, b);
        } else if (!strcmp(op, "unlink")) {
            unlink(argv[++i]);
        } else if (!strcmp(op, "openfail")) {
            int fd = open(argv[++i], O_RDONLY); if (fd >= 0) close(fd);
        } else if (!strcmp(op, "connect")) {
            int s = socket(AF_INET, SOCK_STREAM, 0);
            struct sockaddr_in a = {0}; a.sin_family = AF_INET; a.sin_port = htons((unsigned short)atoi(argv[++i]));
            inet_pton(AF_INET, "127.0.0.1", &a.sin_addr);
            connect(s, (struct sockaddr *)&a, sizeof a); close(s);
        }
    }
    return 0;
}

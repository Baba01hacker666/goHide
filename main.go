// gohide — just run a command and it hides itself as a kernel worker.
//
//	gohide nmap -sCV -F scanme.nmap.org
//
// The Go binary is a tiny launcher: it picks a plausible kworker name, makes
// sure the embedded zapper ptrace engine is available, and execs it. The
// engine wipes argv/env from /proc right after execve(), so ps/top see only
// the fake name. A pure-Go engine is stashed in experimental/go-engine/.
package main

import (
	_ "embed"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed core/zapper.c
var zapperSource string

var version = "dev"

const usage = `gohide %s — run a command disguised as a kernel worker

usage:
  gohide [-no-pt] <command> [args...]

That's it. Options and environment are hidden from ps/top and /proc, and the
process shows up as something like [kworker/3:1-events]. The kernel comm is
spoofed too, so plain top/htop and ps -o comm show the fake name.

  -no-pt            leave the real kernel comm in place

optional environment:
  GOHIDE_NAME=NAME    use a custom name instead of a random kworker
  GOHIDE_PT=0         same as -no-pt
  GOHIDE_FOLLOW=1     hide child processes too
  GOHIDE_KEEP_ENV=1   leave the environment alone
  GOHIDE_PID=N        start under pid N

  gohide version
`

// shortComm makes a <=15 byte kernel comm from a (possibly longer) fake name.
func shortComm(name string) string {
	s := strings.Trim(name, "[]")
	if len(s) > 15 {
		s = s[:15]
	}
	return s
}

// command strips optional leading mode flags / "--" tokens.
func command(argv []string) ([]string, error) {
	if len(argv) > 0 && (argv[0] == "-pt" || argv[0] == "-no-pt" || argv[0] == "--") {
		argv = argv[1:]
	}
	if len(argv) > 0 && argv[0] == "--" {
		argv = argv[1:]
	}
	if len(argv) == 0 {
		return nil, errors.New("no command given")
	}
	return argv, nil
}

var workerSuffixes = []string{"", "", "-events", "-kblockd", "-watchdog", "-rcu_gp"}

// kworkerName makes up a plausible kernel-worker name.
func kworkerName() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano() ^ int64(os.Getpid())))
	cpus := runtime.NumCPU()
	if cpus < 1 {
		cpus = 1
	}
	return fmt.Sprintf("[kworker/%d:%d%s]", r.Intn(cpus), 1+r.Intn(4), workerSuffixes[r.Intn(len(workerSuffixes))])
}

// zapperArgs maps our zero-config CLI onto the engine's getopt CLI. "--"
// keeps a command that itself starts with a dash from being eaten as a flag.
func zapperArgs(bin, name string, pt bool, argv []string) []string {
	args := []string{bin, "-a", name}
	if pt {
		args = append(args, "-p", shortComm(name))
	}
	if os.Getenv("GOHIDE_FOLLOW") == "1" {
		args = append(args, "-f")
	}
	if os.Getenv("GOHIDE_KEEP_ENV") == "1" {
		args = append(args, "-E")
	}
	if pid := os.Getenv("GOHIDE_PID"); pid != "" {
		if _, err := strconv.Atoi(pid); err == nil {
			args = append(args, "-n", pid)
		}
	}
	return append(append(args, "--"), argv...)
}

// ensureZapper returns the path of the ptrace engine. A binary shipped next
// to gohide wins; otherwise the embedded source is compiled into the user's
// cache directory on first run.
func ensureZapper() (string, error) {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, "gohide-zapper"), filepath.Join(dir, "zapper"))
	}
	candidates = append(candidates, "gohide-zapper", "zapper")
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			if abs, err := filepath.Abs(c); err == nil {
				return abs, nil
			}
		}
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("no prebuilt engine and no cache dir: %w", err)
	}
	dir := filepath.Join(cache, "gohide")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	bin := filepath.Join(dir, "zapper")
	if st, err := os.Stat(bin); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
		return bin, nil
	}
	src := filepath.Join(dir, "zapper.c")
	if err := os.WriteFile(src, []byte(zapperSource), 0o644); err != nil {
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
		return "", errors.New("no prebuilt engine and no C compiler on PATH")
	}
	out, err := exec.Command(cc, "-O2", "-D_GNU_SOURCE", "-DZVERSION=\""+strings.TrimPrefix(version, "v")+"\"", "-o", bin, src).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("building engine: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return bin, nil
}

func main() {
	switch {
	case len(os.Args) > 1 && (os.Args[1] == "version" || os.Args[1] == "-v"):
		fmt.Println("gohide", version)
		return
	case len(os.Args) > 1 && (os.Args[1] == "-h" || os.Args[1] == "--help" || os.Args[1] == "help"):
		fmt.Printf(usage, version)
		return
	}

	pt := os.Getenv("GOHIDE_PT") != "0" && !(len(os.Args) > 1 && os.Args[1] == "-no-pt")
	cmd, err := command(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "gohide: %v\n\n%s", err, fmt.Sprintf(usage, version))
		os.Exit(2)
	}
	name := os.Getenv("GOHIDE_NAME")
	if name == "" {
		name = kworkerName()
	}

	bin, err := ensureZapper()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gohide: %v\n", err)
		os.Exit(1)
	}
	if err := syscall.Exec(bin, zapperArgs(bin, name, pt, cmd), os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "gohide: exec %s: %v\n", bin, err)
		os.Exit(1)
	}
}

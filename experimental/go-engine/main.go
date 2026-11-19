// Work-in-progress: pure-Go ptrace engine (no zapper.c, no C compiler).
// Not used by the released gohide binary yet. See README.md here.
package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

// shortComm makes a <=15 byte kernel comm from a (possibly longer) fake name.
func shortComm(name string) string {
	s := strings.Trim(name, "[]")
	if len(s) > 15 {
		s = s[:15]
	}
	return s
}

func main() {
	args := os.Args[1:]
	pt := os.Getenv("GOHIDE_PT") == "1"
	if len(args) > 0 && args[0] == "-pt" {
		pt = true
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: go-engine [-pt] <command> [args...]")
		os.Exit(2)
	}
	name := "[kworker/" + fmt.Sprint(runtime.NumCPU()-1) + ":1]"
	comm := ""
	if pt {
		comm = shortComm(name)
	}
	if err := run(name, comm, os.Getenv("GOHIDE_FOLLOW") == "1", false, args); err != nil {
		fmt.Fprintln(os.Stderr, "go-engine:", err)
		os.Exit(1)
	}
}

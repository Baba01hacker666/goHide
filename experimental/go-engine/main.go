// Work-in-progress: pure-Go ptrace engine (no zapper.c, no C compiler).
// Not used by the released gohide binary yet. See README.md here.
package main

import (
	"fmt"
	"os"
	"runtime"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go-engine <command> [args...]")
		os.Exit(2)
	}
	name := "[kworker/" + fmt.Sprint(runtime.NumCPU()-1) + ":1]"
	if err := run(name, os.Getenv("GOHIDE_FOLLOW") == "1", false, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "go-engine:", err)
		os.Exit(1)
	}
}

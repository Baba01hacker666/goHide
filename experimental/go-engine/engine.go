//go:build linux && amd64

package main

// A small ptrace engine, written in Go, based on the technique used by
// hackerschoice/zapper: stop the tracee right after execve(), move argv/env
// to a fresh part of the stack, destroy the old copy, fix the ELF aux table,
// and let it run. procps sees only the fake name and an empty environment,
// while the program itself keeps its real arguments and environment.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	ptracePeekdata    = 2
	ptracePokedata    = 5
	ptraceCont        = 7
	ptraceSinglestep  = 9
	ptraceGetregs     = 12
	ptraceSetregs     = 13
	ptraceDetach      = 17
	ptraceSyscall     = 24
	ptraceSetoptions  = 0x4200
	ptraceGeteventmsg = 0x4201

	optTraceSysgood = 0x000001
	optTraceFork    = 0x000002
	optTraceVfork   = 0x000004
	optTraceExec    = 0x000008
	optExitkill     = 0x100000

	eventFork  = 1
	eventVfork = 2
	eventExec  = 4

	syscallTrap = 0x85 // SIGTRAP | 0x80 with TRACESYSGOOD

	wordSize = 8

	// AT_ entries whose value is a pointer into the stack string area.
	atPlatform     = 0x0f
	atBasePlatform = 0x18
	atRandom       = 0x19
	atExecfn       = 0x1f
	atSysinfoEhdr  = 0x21
)

func ptraceCall(req uintptr, pid int, addr, data uintptr) (uintptr, error) {
	r, _, errno := syscall.Syscall6(syscall.SYS_PTRACE, req, uintptr(pid), addr, data, 0, 0)
	if errno != 0 {
		return r, errno
	}
	return r, nil
}

func waitpid(pid int, ws *syscall.WaitStatus, opts int) (int, error) {
	r, _, errno := syscall.Syscall6(syscall.SYS_WAIT4, uintptr(pid), uintptr(unsafe.Pointer(ws)), uintptr(opts), 0, 0, 0)
	if errno != 0 {
		return 0, errno
	}
	return int(r), nil
}

func peekWord(pid int, addr uintptr) (uintptr, error) {
	var out uintptr
	_, err := ptraceCall(ptracePeekdata, pid, addr, uintptr(unsafe.Pointer(&out)))
	if err != nil {
		return 0, err
	}
	return out, nil
}

func pokeWord(pid int, addr, val uintptr) error {
	_, err := ptraceCall(ptracePokedata, pid, addr, val)
	return err
}

func getRegs(pid int) (*syscall.PtraceRegs, error) {
	var regs syscall.PtraceRegs
	_, err := ptraceCall(ptraceGetregs, pid, 0, uintptr(unsafe.Pointer(&regs)))
	runtime.KeepAlive(&regs)
	return &regs, err
}

func setRegs(pid int, regs *syscall.PtraceRegs) error {
	_, err := ptraceCall(ptraceSetregs, pid, 0, uintptr(unsafe.Pointer(regs)))
	runtime.KeepAlive(regs)
	return err
}

func setOptions(pid int, follow bool) error {
	opts := uintptr(optTraceSysgood | optExitkill)
	if follow {
		opts |= optTraceFork | optTraceVfork | optTraceExec
	}
	_, err := ptraceCall(ptraceSetoptions, pid, 0, opts)
	return err
}

// readStack copies the tracee's initial stack, word by word, up to the end
// of the mapping. PEEKDATA fails once we walk past it.
func readStack(pid int, sp uintptr, maxWords int) ([]uintptr, error) {
	out := make([]uintptr, 0, 512)
	for i := 0; i < maxWords; i++ {
		w, err := peekWord(pid, sp+uintptr(i)*wordSize)
		if err != nil {
			break // reached the end of the stack mapping
		}
		out = append(out, w)
	}
	if len(out) < 4 {
		return nil, errors.New("could not read the target's initial stack")
	}
	return out, nil
}

func readCString(pid int, addr uintptr, max int) string {
	b := make([]byte, 0, 64)
	var buf [wordSize]byte
	for off := 0; off < max; off += wordSize {
		w, err := peekWord(pid, addr+uintptr(off))
		if err != nil {
			break
		}
		binary.LittleEndian.PutUint64(buf[:], uint64(w))
		for _, c := range buf {
			if c == 0 {
				return string(b)
			}
			b = append(b, c)
		}
	}
	return string(b)
}

func putWord(b []byte, off int, v uintptr) {
	binary.LittleEndian.PutUint64(b[off:], uint64(v))
}

// writeBytes writes a zero-padded whole-word buffer. PTRACE_POKEDATA works a
// word at a time, so partial writes would clobber neighbouring bytes.
func writeBytes(pid int, addr uintptr, b []byte) error {
	buf := append([]byte(nil), b...)
	if rem := len(buf) % wordSize; rem != 0 {
		buf = append(buf, make([]byte, wordSize-rem)...)
	}
	for off := 0; off < len(buf); off += wordSize {
		if err := pokeWord(pid, addr+uintptr(off), uintptr(binary.LittleEndian.Uint64(buf[off:]))); err != nil {
			return err
		}
	}
	return nil
}

// setComm injects prctl(PR_SET_NAME, name) into the tracee. top/htop and
// "ps -o comm" read the kernel comm field, which zapper/argv tricks never
// touch. The name is staged in the wiped env region and erased afterwards.
func setComm(pid int, regs *syscall.PtraceRegs, name string, addr uintptr) error {
	if name == "" || addr == 0 {
		return nil
	}
	comm := make([]byte, 16)
	n := len(name)
	if n > 15 {
		n = 15 // TASK_COMM_LEN - 1
	}
	copy(comm, name[:n])

	rip := uintptr(regs.Rip)
	orig, err := peekWord(pid, rip)
	if err != nil {
		return err
	}
	if err := writeBytes(pid, addr, comm); err != nil {
		return err
	}
	// 0f 05 = syscall on x86_64
	if err := pokeWord(pid, rip, (orig&^uintptr(0xffff))|0x050f); err != nil {
		return err
	}
	call := *regs
	call.Rax = 157 // __NR_prctl
	call.Rdi = 15  // PR_SET_NAME
	call.Rsi = uint64(addr)
	call.Rdx, call.R10, call.R8, call.R9 = 0, 0, 0, 0

	restore := func() {
		_ = pokeWord(pid, rip, orig)
		_ = setRegs(pid, regs)
		_ = writeBytes(pid, addr, make([]byte, 16))
	}
	if err := setRegs(pid, &call); err != nil {
		restore()
		return err
	}
	if _, err := ptraceCall(ptraceSinglestep, pid, 0, 0); err == nil {
		var ws syscall.WaitStatus
		_, _ = waitpid(pid, &ws, 0)
	}
	restore()
	return nil
}

type stackLayout struct {
	words       []uintptr
	sp          uintptr
	argc        int
	argvIdx     int
	envIdx      int
	envCount    int
	auxIdx      int
	argMin      uintptr
	argMax      uintptr
	envMin      uintptr
	envMax      uintptr
	stringStart uintptr
	stringEnd   uintptr
}

func parseStack(pid int, sp uintptr, words []uintptr) (*stackLayout, error) {
	argc := int(words[0])
	if argc < 0 || argc+2 > len(words) {
		return nil, errors.New("invalid argc on the target stack")
	}
	l := &stackLayout{words: words, sp: sp, argc: argc, argvIdx: 1}
	idx := 1 + argc
	if idx >= len(words) || words[idx] != 0 {
		return nil, errors.New("argv is not NUL-terminated")
	}
	idx++
	l.envIdx = idx
	for idx < len(words) && words[idx] != 0 {
		idx++
	}
	l.envCount = idx - l.envIdx
	if idx < len(words) {
		idx++ // envp NULL
	}
	l.auxIdx = idx

	l.argMin, l.argMax = ^uintptr(0), 0
	for i := 0; i < argc; i++ {
		p := words[l.argvIdx+i]
		if p == 0 {
			continue
		}
		end := p + uintptr(len(readCString(pid, p, 4096))+1)
		if p < l.argMin {
			l.argMin = p
		}
		if end > l.argMax {
			l.argMax = end
		}
	}
	l.envMin, l.envMax = ^uintptr(0), 0
	for i := 0; i < l.envCount; i++ {
		p := words[l.envIdx+i]
		if p == 0 {
			continue
		}
		end := p + uintptr(len(readCString(pid, p, 65536))+1)
		if p < l.envMin {
			l.envMin = p
		}
		if end > l.envMax {
			l.envMax = end
		}
	}
	if l.argMin == ^uintptr(0) {
		return nil, errors.New("no argv strings found")
	}
	l.stringStart = l.argMin
	l.stringEnd = l.argMax
	if l.envCount > 0 {
		if l.envMin < l.stringStart {
			l.stringStart = l.envMin
		}
		if l.envMax > l.stringEnd {
			l.stringEnd = l.envMax
		}
	}
	return l, nil
}

// rebuildStack creates the new initial stack image. Pointers are shifted down
// by gap so the program still finds its real strings, while the kernel's
// recorded cmdline/environ addresses are overwritten with the fake name and
// zeroes. That's what ps and /proc read.
func rebuildStack(l *stackLayout, name string, keepEnv bool) ([]byte, uintptr, error) {
	oldSz := uintptr(len(l.words) * wordSize)
	gap := l.stringEnd - l.stringStart
	total := oldSz + gap
	if total&15 != 0 {
		total = (total + 16) &^ 15
	}
	gap = total - oldSz

	buf := make([]byte, int(total))
	for i, w := range l.words {
		putWord(buf, i*wordSize, w)
	}
	// Preserve the original top-of-stack bytes at their original addresses.
	copy(buf[int(oldSz):], buf[int(oldSz-gap):int(oldSz)])

	// Fix auxv entries that point into the moved string area.
	for i := l.auxIdx; i+1 < len(l.words); i += 2 {
		typ, val := l.words[i], l.words[i+1]
		if typ == 0 {
			break
		}
		switch typ {
		case atPlatform, atBasePlatform, atRandom, atExecfn, atSysinfoEhdr:
			if val != 0 && val >= l.sp && val-l.sp <= oldSz {
				putWord(buf, (i+1)*wordSize, val-gap)
			}
		}
	}

	// Point the program's argv/env at the relocated strings.
	for i := 0; i < l.argc; i++ {
		p := l.words[l.argvIdx+i]
		if p != 0 {
			putWord(buf, (l.argvIdx+i)*wordSize, p-gap)
		}
	}
	for i := 0; i < l.envCount; i++ {
		p := l.words[l.envIdx+i]
		if p != 0 {
			putWord(buf, (l.envIdx+i)*wordSize, p-gap)
		}
	}

	// Wipe the regions the kernel reports as cmdline/environ.
	clearRange := func(lo, hi uintptr) {
		if hi <= lo {
			return
		}
		start := lo - l.sp + gap
		n := hi - lo
		if start > uintptr(len(buf)) {
			return
		}
		if start+n > uintptr(len(buf)) {
			n = uintptr(len(buf)) - start
		}
		clear(buf[int(start):int(start+n)])
	}
	clearRange(l.argMin, l.argMax)
	if !keepEnv && l.envCount > 0 {
		clearRange(l.envMin, l.envMax)
	}

	// Stamp the fake name over the original argv[0] string.
	argv0 := l.words[l.argvIdx]
	room := l.stringEnd - l.stringStart
	if room == 0 {
		return nil, 0, errors.New("no room for the fake name")
	}
	if room > 256 {
		room = 256 // keep it plausible and bounded
	}
	b := []byte(name)
	if len(b) > int(room)-1 {
		b = b[:room-1]
	}
	off := argv0 - l.sp + gap
	if off+uintptr(len(b)) >= uintptr(len(buf)) {
		return nil, 0, errors.New("fake name does not fit on the stack")
	}
	copy(buf[int(off):], b)
	buf[int(off)+len(b)] = 0

	return buf, gap, nil
}

// zap rewrites one tracee's initial stack. The tracee must be stopped just
// after execve() with its registers available.
func zap(pid int, name, comm string, keepEnv bool) error {
	regs, err := getRegs(pid)
	if err != nil {
		return fmt.Errorf("getregs: %w", err)
	}
	sp := uintptr(regs.Rsp)
	words, err := readStack(pid, sp, 8192)
	if err != nil {
		return err
	}
	l, err := parseStack(pid, sp, words)
	if err != nil {
		return err
	}
	buf, gap, err := rebuildStack(l, name, keepEnv)
	if err != nil {
		return err
	}
	regs.Rsp -= uint64(gap)
	if err := setRegs(pid, regs); err != nil {
		return fmt.Errorf("setregs: %w", err)
	}
	newSP := uintptr(regs.Rsp)
	for off := 0; off < len(buf); off += wordSize {
		if err := pokeWord(pid, newSP+uintptr(off), uintptr(binary.LittleEndian.Uint64(buf[off:]))); err != nil {
			return fmt.Errorf("pokedata at %#x: %w", newSP+uintptr(off), err)
		}
	}
	if comm != "" {
		addr := l.envMin // wiped env region
		if addr == 0 {
			addr = l.argMin
		}
		if err := setComm(pid, regs, comm, addr); err != nil {
			return fmt.Errorf("comm: %w", err)
		}
	}
	return nil
}

// run starts the command under ptrace, zaps it, and waits for it.
func run(name, comm string, follow, keepEnv bool, argv []string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	proc, err := os.StartProcess(argv[0], argv, &os.ProcAttr{
		Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		Sys:   &syscall.SysProcAttr{Ptrace: true},
	})
	if err != nil {
		return fmt.Errorf("start %s: %w", argv[0], err)
	}
	pid := proc.Pid

	// First stop: execve() completed with TRACEME attached.
	var ws syscall.WaitStatus
	if _, err := waitpid(pid, &ws, 0); err != nil {
		return fmt.Errorf("wait for exec: %w", err)
	}
	if err := setOptions(pid, follow); err != nil {
		return fmt.Errorf("setoptions: %w", err)
	}
	if err := zap(pid, name, comm, keepEnv); err != nil {
		_, _ = ptraceCall(ptraceDetach, pid, 0, 0)
		_, _ = proc.Wait()
		return err
	}

	if !follow {
		_, _ = ptraceCall(ptraceDetach, pid, 0, 0)
		state, err := proc.Wait()
		if err != nil {
			return err
		}
		if code := state.ExitCode(); code != 0 {
			os.Exit(code)
		}
		return nil
	}

	return followTrace(pid, name, comm, keepEnv)
}

// followTrace keeps tracing fork/exec events and zaps every new program.
func followTrace(root int, name, comm string, keepEnv bool) error {
	pending := map[int]bool{}
	if _, err := ptraceCall(ptraceCont, root, 0, 0); err != nil {
		return err
	}
	for {
		var ws syscall.WaitStatus
		pid, err := waitpid(-1, &ws, 0)
		if err != nil {
			if errors.Is(err, syscall.EINTR) {
				continue
			}
			return err
		}
		if pid <= 0 {
			continue
		}
		if ws.Exited() || ws.Signaled() {
			if pid == root {
				if ws.Exited() {
					if code := ws.ExitStatus(); code != 0 {
						os.Exit(code)
					}
				} else {
					os.Exit(128 + int(ws.Signal()))
				}
				return nil
			}
			continue
		}
		if !ws.Stopped() {
			continue
		}

		event := uint32(ws) >> 16 // WaitStatus packs ptrace event in bits 16..31
		sig := int(ws.StopSignal())

		switch {
		case event == eventFork || event == eventVfork:
			var child uintptr
			if _, err := ptraceCall(ptraceGeteventmsg, pid, 0, uintptr(unsafe.Pointer(&child))); err == nil && child > 0 {
				var cws syscall.WaitStatus
				_, _ = waitpid(int(child), &cws, syscall.WNOHANG|syscall.WUNTRACED)
				if err := setOptions(int(child), true); err == nil {
					_, _ = ptraceCall(ptraceCont, int(child), 0, 0)
				}
			}
			_, _ = ptraceCall(ptraceCont, pid, 0, 0)

		case event == eventExec:
			pending[pid] = true
			_, _ = ptraceCall(ptraceSyscall, pid, 0, 0)

		case sig == syscallTrap:
			if pending[pid] {
				delete(pending, pid)
				if err := zap(pid, name, comm, keepEnv); err != nil {
					fmt.Fprintf(os.Stderr, "gohide: zap child %d: %v\n", pid, err)
				}
			}
			_, _ = ptraceCall(ptraceSyscall, pid, 0, 0)

		default:
			_, _ = ptraceCall(ptraceCont, pid, 0, uintptr(sig))
		}
	}
}

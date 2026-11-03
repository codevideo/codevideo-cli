package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rogpeppe/go-internal/testscript"
)

// sigterm pidfile command [args...] runs a command, sends it SIGTERM once
// pidfile exists (pass the fake runner's $FAKE_PID_FILE), and waits for it;
// stdout and stderr are the command's. Only the command gets the signal, as
// when another program cancels the CLI, so any other process that stops
// heard about it from the CLI. sigterm fails unless the command exits 130.
func sigterm(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) < 2 {
		ts.Fatalf("usage: sigterm pidfile command [args...]")
	}
	pidFile := ts.MkAbs(args[0])
	if err := os.Remove(pidFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		ts.Fatalf("%v", err)
	}
	path, err := lookPath(ts, args[1])
	ts.Check(err)
	var stdout, stderr strings.Builder
	cmd := exec.Command(path, args[2:]...)
	cmd.Dir = ts.MkAbs(".")
	cmd.Env = scriptEnv(ts)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	ts.Check(cmd.Start())
	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()
	fail := func(format string, args ...any) {
		cmd.Process.Kill()
		<-exited
		ts.Logf("[stderr]\n%s", stderr.String())
		ts.Fatalf(format, args...)
	}

	for started := time.Now(); !fileExists(pidFile); time.Sleep(50 * time.Millisecond) {
		select {
		case <-exited:
			ts.Logf("[stderr]\n%s", stderr.String())
			ts.Fatalf("%s exited before %s was written", args[1], args[0])
		default:
		}
		if time.Since(started) > 30*time.Second {
			fail("no %s after 30s", args[0])
		}
	}
	ts.Check(cmd.Process.Signal(syscall.SIGTERM))
	select {
	case <-exited:
	case <-time.After(30 * time.Second):
		fail("%s still running 30s after SIGTERM", args[1])
	}
	fmt.Fprint(ts.Stdout(), stdout.String())
	fmt.Fprint(ts.Stderr(), stderr.String())
	if code := cmd.ProcessState.ExitCode(); code != 130 {
		ts.Fatalf("%s exited with %d, want 130", args[1], code)
	}
}

// gone pidfile fails unless every process pidfile lists has exited, allowing
// 5 seconds. It kills any still running, so a failure leaves none behind.
func gone(ts *testscript.TestScript, neg bool, args []string) {
	if neg || len(args) != 1 {
		ts.Fatalf("usage: gone pidfile")
	}
	var pids []int
	for _, field := range strings.Fields(ts.ReadFile(args[0])) {
		pid, err := strconv.Atoi(field)
		ts.Check(err)
		pids = append(pids, pid)
	}
	if len(pids) == 0 {
		ts.Fatalf("%s lists no processes", args[0])
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		var running []int
		for _, pid := range pids {
			if processRunning(pid) {
				running = append(running, pid)
			}
		}
		if len(running) == 0 {
			return
		}
		if time.Now().After(deadline) {
			for _, pid := range running {
				if p, err := os.FindProcess(pid); err == nil {
					p.Kill()
					p.Release()
				}
			}
			ts.Fatalf("still running after 5s: %v", running)
		}
	}
}

// processRunning reports whether a process exists, by sending it signal 0.
func processRunning(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	defer p.Release()
	return p.Signal(syscall.Signal(0)) == nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

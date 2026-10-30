package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/charmbracelet/x/xpty"
	"github.com/rogpeppe/go-internal/testscript"
)

const ttyHeight = 30

// ttyrun [-width N] command [args...] runs a command on a pseudo-terminal (a
// Unix pty, or ConPTY on Windows) the way a user's terminal would, replays
// what it prints through a terminal emulator, and writes what the screen
// showed: screen.txt at the end and screens.txt after every change. Times read
// m:ss and \ reads / so the files match on every machine.
//
// Comparing screens rather than bytes is what makes this work on Windows:
// ConPTY redraws the screen with its own escape sequences instead of passing
// the program's through.
func ttyrun(ts *testscript.TestScript, neg bool, args []string) {
	width := 80
	if len(args) >= 2 && args[0] == "-width" {
		w, err := strconv.Atoi(args[1])
		ts.Check(err)
		width, args = w, args[2:]
	}
	if len(args) == 0 {
		ts.Fatalf("usage: ttyrun [-width N] command [args...]")
	}
	path, err := lookPath(ts, args[0])
	ts.Check(err)

	pty, err := xpty.NewPty(width, ttyHeight)
	ts.Check(err)
	defer pty.Close()
	cmd := exec.Command(path, args[1:]...)
	cmd.Dir = ts.MkAbs(".")
	cmd.Env = append(scriptEnv(ts), "TERM=xterm-256color")
	ts.Check(pty.Start(cmd))
	if unix, ok := pty.(*xpty.UnixPty); ok {
		// The child has its own copy. With ours closed, reads end when it exits.
		unix.Slave().Close()
	}

	term := vt.NewEmulator(width, ttyHeight)
	defer term.Close()
	// Answer the program the way a terminal would (cursor position reports,
	// for example); ConPTY asks some questions of its own.
	go io.Copy(pty, term)

	var screens []string
	read := make(chan struct{})
	go func() {
		defer close(read)
		buf := make([]byte, 32*1024)
		for {
			n, err := pty.Read(buf)
			if n > 0 {
				term.Write(buf[:n])
				if screen := normalizeScreen(term.String()); len(screens) == 0 || screens[len(screens)-1] != screen {
					screens = append(screens, screen)
				}
			}
			if err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	waitErr := xpty.WaitProcess(ctx, cmd)
	if errors.Is(waitErr, context.DeadlineExceeded) {
		ts.Fatalf("%s did not finish within 2 minutes", args[0])
	}
	// A Unix pty's reads end on their own once the program exits. ConPTY sends
	// its last output, and EOF, only when it's closed.
	select {
	case <-read:
	case <-time.After(time.Second):
		pty.Close()
		<-read
	}

	screen := normalizeScreen(term.String())
	ts.Logf("[screen]\n%s", screen)
	ts.Check(os.WriteFile(ts.MkAbs("screen.txt"), []byte(screen), 0644))
	ts.Check(os.WriteFile(ts.MkAbs("screens.txt"), []byte(strings.Join(screens, "────\n")), 0644))
	switch {
	case neg && waitErr == nil:
		ts.Fatalf("%s succeeded unexpectedly", args[0])
	case !neg && waitErr != nil:
		ts.Fatalf("%s: %v", args[0], waitErr)
	}
}

var (
	clockPattern     = regexp.MustCompile(`\b\d+:\d\d(:\d\d)?\b`)
	savedFilePattern = regexp.MustCompile(`CodeVideo-\d{4}(-\d\d){5}`)
)

// normalizeScreen writes times as m:ss, default file names' timestamps as
// YYYY-MM-DD-hh-mm-ss and paths with /, and trims trailing spaces and blank
// lines.
func normalizeScreen(screen string) string {
	lines := strings.Split(strings.ReplaceAll(screen, `\`, "/"), "\n")
	for i, line := range lines {
		line = savedFilePattern.ReplaceAllString(line, "CodeVideo-YYYY-MM-DD-hh-mm-ss")
		lines[i] = strings.TrimRight(clockPattern.ReplaceAllString(line, "m:ss"), " ")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

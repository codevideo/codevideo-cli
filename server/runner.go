package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/codevideo/codevideo-cli/cli/progress"
	log "github.com/sirupsen/logrus"
)

// The Node runners report progress as one JSON object per stdout line, after
// this prefix. Everything else they print goes to the debug log.
const progressPrefix = "CODEVIDEO_PROGRESS "

// runnerEvent is one progress line.
type runnerEvent struct {
	Stage  string `json:"stage"`  // "recording", "closing", "planning" or "frames"
	Action *int   `json:"action"` // recording: the action that started playing
	Frame  int    `json:"frame"`  // planning: frames evaluated; frames: frames captured
	Frames int    `json:"frames"` // frames: frames to capture
	Error  string `json:"error"`  // the runner failed with this message
}

func parseRunnerEvent(line string) (runnerEvent, bool) {
	rest, ok := strings.CutPrefix(line, progressPrefix)
	if !ok {
		return runnerEvent{}, false
	}
	var event runnerEvent
	if err := json.Unmarshal([]byte(rest), &event); err != nil {
		return runnerEvent{}, false
	}
	return event, true
}

func (e runnerEvent) apply(job *progress.Job) {
	switch e.Stage {
	case "recording":
		job.Begin(progress.Recording)
		if e.Action != nil {
			job.ActionStarted(*e.Action)
		}
	case "closing":
		job.Begin(progress.Closing)
	case "planning":
		job.Begin(progress.Planning)
		job.PlanningFrame(e.Frame)
	case "frames":
		job.Begin(progress.Frames)
		job.FramesDone(e.Frame, e.Frames)
	}
}

// runnerOutput routes a runner's output and remembers what explains a failure:
// the runner's own error message, or else the last lines of its stderr.
type runnerOutput struct {
	job     *progress.Job
	name    string
	mu      sync.Mutex
	failure string
	tail    []string
}

const tailLines = 5

func (o *runnerOutput) stdoutLine(line string) {
	if event, ok := parseRunnerEvent(line); ok {
		if event.Error != "" {
			o.mu.Lock()
			o.failure = event.Error
			o.mu.Unlock()
		}
		event.apply(o.job)
		return
	}
	log.Debugf("[%s stdout]: %s", o.name, line)
}

func (o *runnerOutput) stderrLine(line string) {
	log.Debugf("[%s stderr]: %s", o.name, line)
	if strings.TrimSpace(line) == "" {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.tail = append(o.tail, strings.TrimSpace(line))
	if len(o.tail) > tailLines {
		o.tail = o.tail[len(o.tail)-tailLines:]
	}
}

func (o *runnerOutput) explain(err error) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	switch {
	case o.failure != "":
		return fmt.Errorf("%w: %s", err, o.failure)
	case len(o.tail) > 0:
		return fmt.Errorf("%w: %s", err, strings.Join(o.tail, " | "))
	}
	return err
}

// runRunner runs a Node runner to completion, feeding its progress lines to
// job and logging the rest at debug level.
func runRunner(cmd *exec.Cmd, name string, job *progress.Job) error {
	output := &runnerOutput{job: job, name: name}
	stdout := &lineWriter{line: output.stdoutLine}
	stderr := &lineWriter{line: output.stderrLine}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	stdout.flush()
	stderr.flush()
	if err != nil {
		return output.explain(err)
	}
	return nil
}

// lineWriter calls line for each complete line written to it. exec.Cmd copies
// each stream from a single goroutine and waits for the copy before Wait
// returns, so no output is lost or read after the process is reaped.
type lineWriter struct {
	line    func(string)
	pending []byte
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.pending = append(w.pending, p...)
	for {
		end := bytes.IndexByte(w.pending, '\n')
		if end < 0 {
			break
		}
		w.line(strings.TrimRight(string(w.pending[:end]), "\r"))
		w.pending = w.pending[end+1:]
	}
	return len(p), nil
}

func (w *lineWriter) flush() {
	if len(w.pending) > 0 {
		w.line(string(w.pending))
		w.pending = nil
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Stand-ins for the programs the CLI runs. Each does just enough for the CLI:
// the progress lines and output files it reads, plus some chatter for the
// debug log. TestMain puts them on the scripts' PATH as node, ffmpeg and
// ffprobe.

// fakeStep waits for one simulated step: FAKE_STEP_MS, or 150ms.
func fakeStep() {
	ms, err := strconv.Atoi(os.Getenv("FAKE_STEP_MS"))
	if err != nil {
		ms = 150
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
}

func emitProgress(event map[string]any) {
	line, _ := json.Marshal(event)
	fmt.Println("CODEVIDEO_PROGRESS " + string(line))
}

// runnerFlags reads "--name value" pairs and bare "--name" switches.
func runnerFlags(args []string) map[string]string {
	flags := map[string]string{}
	for i := 0; i < len(args); i++ {
		name, ok := strings.CutPrefix(args[i], "--")
		if !ok {
			continue
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			flags[name] = args[i+1]
			i++
		} else {
			flags[name] = "true"
		}
	}
	return flags
}

// fakeNode plays the Node runner named by its first argument. FAKE_RUNNER
// picks a failure: "chrome" fails the way a bad CODEVIDEO_CHROME_PATH does,
// "crash" dies without explaining itself.
func fakeNode() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "fake node: no script")
		os.Exit(2)
	}
	// The CLI logs this at debug level, so scripts can check the arguments.
	fmt.Println("fake node", strings.Join(args, " "))
	flags := runnerFlags(args[1:])
	switch filepath.Base(args[0]) {
	case "recordVideoV3.js":
		os.Exit(fakeRecorder(flags))
	case "renderFramesV1.js":
		os.Exit(fakeFrameRenderer(flags))
	}
	fmt.Fprintln(os.Stderr, "fake node: unknown script", args[0])
	os.Exit(2)
}

// fakeRecorder plays recordVideoV3.js: the browser starts, the recording
// starts, each action plays, the webm is saved, the browser closes.
func fakeRecorder(flags map[string]string) int {
	switch os.Getenv("FAKE_RUNNER") {
	case "chrome":
		const message = "CODEVIDEO_CHROME_PATH is not an executable file: /nonexistent/chrome"
		fmt.Fprintln(os.Stderr, "Error during recording: Error: "+message)
		emitProgress(map[string]any{"error": message})
		return 1
	case "crash":
		fmt.Fprintln(os.Stderr, "TypeError: Cannot read properties of undefined (reading 'evaluate')")
		fmt.Fprintln(os.Stderr, "    at recordVideoV3 (recordVideoV3.js:215:20)")
		return 1
	}
	actions, err := manifestActionCount(flags["manifest-path"])
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake node:", err)
		return 1
	}
	fakeStep() // the browser and page starting
	emitProgress(map[string]any{"stage": "recording"})
	fakeStep()
	for i := 0; i < actions; i++ {
		emitProgress(map[string]any{"stage": "recording", "action": i})
		fakeStep()
	}
	// The page reports the end of playback as one past the last action.
	emitProgress(map[string]any{"stage": "recording", "action": actions})
	if err := os.WriteFile(flags["output-webm"], []byte("fake webm"), 0644); err != nil {
		fmt.Fprintln(os.Stderr, "fake node:", err)
		return 1
	}
	emitProgress(map[string]any{"stage": "closing"})
	fakeStep()
	return 0
}

// fakeFrameRenderer plays renderFramesV1.js: a planning pass over the
// timeline, then the frame capture, then the trace and metrics files.
func fakeFrameRenderer(flags map[string]string) int {
	actions, err := manifestActionCount(flags["manifest-path"])
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake node:", err)
		return 1
	}
	// A second for each action plus a second of lead-in and one after.
	frames := 60 * (actions + 2)
	report := flags["progress"] == "true"
	for _, frame := range []int{1, frames / 2, frames} {
		if report {
			emitProgress(map[string]any{"stage": "planning", "frame": frame})
		}
		fakeStep()
	}
	fmt.Printf("Deterministic renderer: capturing %d frames with 1 worker(s)\n", frames)
	for _, frame := range []int{0, frames / 2, frames} {
		if report {
			emitProgress(map[string]any{"stage": "frames", "frame": frame, "frames": frames})
		}
		fakeStep()
	}
	trace, _ := json.Marshal(map[string]any{
		"version": 1, "renderMode": "deterministic", "fps": 60,
		"frameCount": frames, "durationMs": float64(frames) * 1000 / 60, "speechCues": []any{},
	})
	for path, data := range map[string][]byte{
		flags["output-mp4"]:   []byte("fake mp4"),
		flags["trace-path"]:   trace,
		flags["metrics-path"]: []byte(fmt.Sprintf(`{"frameCount":%d}`, frames)),
	} {
		if err := os.WriteFile(path, data, 0644); err != nil {
			fmt.Fprintln(os.Stderr, "fake node:", err)
			return 1
		}
	}
	return 0
}

func manifestActionCount(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var manifest struct {
		Actions []json.RawMessage `json:"actions"`
		Lesson  struct {
			Actions []json.RawMessage `json:"actions"`
		} `json:"lesson"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return 0, err
	}
	if len(manifest.Actions) > 0 {
		return len(manifest.Actions), nil
	}
	return len(manifest.Lesson.Actions), nil
}

// fakeFFmpeg writes its output file (the last argument). Asked for -progress,
// as the webm to mp4 conversion does, it reports four blocks on stdout.
func fakeFFmpeg() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "fake ffmpeg: no arguments")
		os.Exit(1)
	}
	if slices.Contains(args, "-progress") {
		for block := 1; block <= 4; block++ {
			state := "continue"
			if block == 4 {
				state = "end"
			}
			fmt.Printf("frame=%d\nout_time_us=%d\nspeed=1.5x\nprogress=%s\n", block*30, block*500000, state)
			fakeStep()
		}
	}
	if err := os.WriteFile(args[len(args)-1], []byte("fake mp4"), 0644); err != nil {
		fmt.Fprintln(os.Stderr, "fake ffmpeg:", err)
		os.Exit(1)
	}
}

// fakeFFprobe answers the two questions the CLI asks: a file's duration, and
// a video's packet timestamps (for webm files that carry no duration).
func fakeFFprobe() {
	args := strings.Join(os.Args[1:], " ")
	switch {
	case strings.Contains(args, "packet=pts_time"):
		for _, pts := range []string{"0.000", "0.500", "1.000", "1.500", "2.000"} {
			fmt.Println(pts)
		}
	case strings.Contains(args, "format=duration"):
		fmt.Println(`{"format":{"duration":"1.200000"}}`)
	default:
		fmt.Fprintln(os.Stderr, "fake ffprobe: unexpected arguments:", args)
		os.Exit(1)
	}
}

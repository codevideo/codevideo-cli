// Package estimate predicts how long each action plays in a realtime recording.
//
// It mirrors codevideo-ide-react's executeActionPlaybackForMonacoInstance, so
// change both together. The TypeScript twin is estimateActionDurationsMs in
// codevideo-types; both are checked against the same fixture
// (testdata/estimate-cases.json here, __tests__/fixtures/estimate-cases.json
// there).
package estimate

import (
	"strings"
	"unicode/utf16"

	"github.com/codevideo/codevideo-cli/types"
)

// Defaults match codevideo-ide-react's CodeVideoIDEConstants.
const (
	DefaultKeyboardTypingPauseMs = 50.0
	DefaultStandardPauseMs       = 1000.0
	// The CLI renders through the embedded /v3 page, which sets its own
	// defaults (example/src/pages/v3.tsx in codevideo-ide-react).
	PageKeyboardTypingPauseMs = 40.0
	PageStandardPauseMs       = 200.0
	// Narration without a measured clip is estimated at 150 words per minute.
	wordsPerSecond = 2.5
	// author-wait falls back to this when its value isn't a positive number.
	defaultWaitMs = 1000.0
	// mouse-move-* pads the standard pause so the cursor animation can finish.
	mouseMovePaddingMs = 300.0
)

// Options are the playback settings that change timing.
type Options struct {
	KeyboardTypingPauseMs float64
	StandardPauseMs       float64
	// SpeechMs holds measured narration lengths by action index. Missing
	// entries are estimated from the word count.
	SpeechMs map[int]float64
}

// OptionsFromProps gives a CLI render's pauses: the /v3 page's defaults,
// overridden by the IDE props from a config file.
func OptionsFromProps(props *types.CodeVideoIDEProps) Options {
	o := Options{KeyboardTypingPauseMs: PageKeyboardTypingPauseMs, StandardPauseMs: PageStandardPauseMs}
	if props == nil {
		return o
	}
	if props.KeyboardTypingPauseMs != nil && *props.KeyboardTypingPauseMs > 0 {
		o.KeyboardTypingPauseMs = float64(*props.KeyboardTypingPauseMs)
	}
	if props.StandardPauseMs != nil && *props.StandardPauseMs > 0 {
		o.StandardPauseMs = float64(*props.StandardPauseMs)
	}
	return o
}

// ActionDurationsMs returns each action's expected playback time in ms.
func ActionDurationsMs(actions []types.Action, o Options) []float64 {
	if o.KeyboardTypingPauseMs <= 0 {
		o.KeyboardTypingPauseMs = DefaultKeyboardTypingPauseMs
	}
	if o.StandardPauseMs <= 0 {
		o.StandardPauseMs = DefaultStandardPauseMs
	}
	durations := make([]float64, len(actions))
	for i, action := range actions {
		// Every action first waits a standard pause so the DOM can settle.
		ms := o.StandardPauseMs
		times := 1
		if repeatable[action.Name] {
			times, _ = parseInt(action.Value)
		}
		for r := 0; r < times; r++ {
			ms += perRunMs(action, i, o)
		}
		durations[i] = ms
	}
	return durations
}

// TotalMs is the sum of ActionDurationsMs.
func TotalMs(actions []types.Action, o Options) float64 {
	total := 0.0
	for _, ms := range ActionDurationsMs(actions, o) {
		total += ms
	}
	return total
}

func perRunMs(action types.Action, index int, o Options) float64 {
	name := action.Name
	switch {
	case name == "author-speak-before" || name == "author-speak-after":
		if ms, ok := o.SpeechMs[index]; ok && ms > 0 {
			return ms
		}
		return float64(len(strings.Fields(action.Value))) / wordsPerSecond * 1000
	case name == "author-wait":
		if ms, ok := parseInt(action.Value); ok && ms > 0 {
			return float64(ms)
		}
		return defaultWaitMs
	case name == "editor-type":
		return float64(jsLength(action.Value)) * o.KeyboardTypingPauseMs
	case name == "terminal-type":
		lines := strings.Split(action.Value, "\n")
		return float64(jsLength(lines[len(lines)-1])) * o.KeyboardTypingPauseMs
	case keyPress[name]:
		return o.KeyboardTypingPauseMs
	case name == "editor-scroll-up" || name == "editor-scroll-down":
		return o.StandardPauseMs
	case strings.HasPrefix(name, "mouse-move-"):
		return o.StandardPauseMs + mouseMovePaddingMs
	}
	// author-speak-during, file explorer typing (not awaited in realtime),
	// clicks, slides and external pages add nothing beyond the base pause.
	return 0
}

// keyPress lists the editor keys that take one typing pause per press.
var keyPress = map[string]bool{
	"editor-arrow-up": true, "editor-arrow-down": true, "editor-arrow-left": true, "editor-arrow-right": true,
	"editor-shift+arrow-right": true, "editor-shift+arrow-left": true, "editor-shift+arrow-up": true, "editor-shift+arrow-down": true,
	"editor-command+shift+arrow-right": true, "editor-command+shift+arrow-left": true,
	"editor-shift+end": true, "editor-shift+home": true,
	"editor-command-left": true, "editor-command-right": true,
	"editor-tab": true, "editor-enter": true, "editor-space": true, "editor-backspace": true,
}

// repeatable mirrors codevideo-types' RepeatableActionNames: these actions run
// as many times as their value says.
var repeatable = map[string]bool{
	"editor-save": true, "editor-arrow-up": true, "editor-arrow-down": true, "editor-arrow-left": true, "editor-arrow-right": true,
	"editor-enter": true, "editor-tab": true,
	"editor-shift+arrow-left": true, "editor-shift+arrow-right": true, "editor-shift+arrow-down": true, "editor-shift+arrow-up": true,
	"editor-cmd+d": true, "editor-backspace": true, "editor-space": true,
	"editor-command+shift+arrow-right": true, "editor-command+shift+arrow-left": true, "editor-shift+end": true, "editor-shift+home": true,
	"editor-command-left": true, "editor-command-right": true, "editor-command-d": true, "editor-command-c": true, "editor-command-v": true,
	"editor-shift-down-arrow": true, "editor-shift-up-arrow": true,
	"mouse-move-file-explorer": true, "mouse-move-editor": true, "mouse-move-terminal": true,
	"mouse-move-file-explorer-context-menu-new-file": true, "mouse-move-file-explorer-context-menu-new-folder": true,
	"mouse-move-file-explorer-file-context-menu-rename": true, "mouse-move-file-explorer-file-context-menu-delete": true,
	"mouse-move-file-explorer-folder-context-menu-new-file": true, "mouse-move-file-explorer-folder-context-menu-new-folder": true,
	"mouse-move-file-explorer-folder-context-menu-rename": true, "mouse-move-file-explorer-folder-context-menu-delete": true,
	"mouse-move-editor-context-menu-go-to-definition": true, "mouse-move-editor-context-menu-cut": true,
	"mouse-move-editor-context-menu-copy": true, "mouse-move-editor-context-menu-paste": true,
	"mouse-move-unsaved-changes-dialog-button-save": true, "mouse-move-unsaved-changes-dialog-button-dont-save": true,
	"mouse-move-unsaved-changes-dialog-button-cancel": true,
	"mouse-left-click": true, "mouse-double-left-click": true, "mouse-triple-left-click": true,
	"mouse-right-click": true, "mouse-right-double-click": true, "mouse-right-triple-click": true,
	"file-explorer-enter-new-file-input": true, "file-explorer-enter-new-folder-input": true,
	"file-explorer-enter-rename-file-input": true, "file-explorer-enter-rename-folder-input": true,
	"terminal-arrow-up": true, "terminal-arrow-down": true, "terminal-arrow-left": true, "terminal-arrow-right": true,
	"terminal-enter": true, "terminal-tab": true, "terminal-shift+arrow-right": true, "terminal-shift+arrow-left": true,
	"terminal-backspace": true, "terminal-space": true, "terminal-command-left": true, "terminal-command-right": true,
	"terminal-command-c": true, "terminal-command-v": true,
}

// parseInt follows JavaScript's parseInt(value, 10): optional leading
// whitespace and sign, then digits, ignoring anything after them.
func parseInt(s string) (int, bool) {
	s = strings.TrimLeft(s, " \t\n\r")
	sign := 1
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
		if s[0] == '-' {
			sign = -1
		}
		s = s[1:]
	}
	n, digits := 0, 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
		digits++
	}
	return sign * n, digits > 0
}

// jsLength counts UTF-16 code units, like JavaScript's string length, since
// the player types value.split("") one unit at a time.
func jsLength(s string) int {
	return len(utf16.Encode([]rune(s)))
}

package estimate

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"testing"

	"github.com/codevideo/codevideo-cli/types"
)

type fixture struct {
	Cases []struct {
		Name    string `json:"name"`
		Options struct {
			KeyboardTypingPauseMs float64            `json:"keyboardTypingPauseMs"`
			StandardPauseMs       float64            `json:"standardPauseMs"`
			SpeechMs              map[string]float64 `json:"speechMs"`
		} `json:"options"`
		Actions    []types.Action `json:"actions"`
		ExpectedMs []float64      `json:"expectedMs"`
	} `json:"cases"`
}

// The same fixture runs in codevideo-types' Jest suite, so the Go and
// TypeScript estimates can't drift apart silently.
func TestActionDurationsMatchSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/estimate-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	for _, c := range f.Cases {
		speech := map[int]float64{}
		for key, ms := range c.Options.SpeechMs {
			index, err := strconv.Atoi(key)
			if err != nil {
				t.Fatalf("%s: bad speechMs key %q", c.Name, key)
			}
			speech[index] = ms
		}
		got := ActionDurationsMs(c.Actions, Options{
			KeyboardTypingPauseMs: c.Options.KeyboardTypingPauseMs,
			StandardPauseMs:       c.Options.StandardPauseMs,
			SpeechMs:              speech,
		})
		if len(got) != len(c.ExpectedMs) {
			t.Fatalf("%s: got %d durations, want %d", c.Name, len(got), len(c.ExpectedMs))
		}
		for i := range got {
			if math.Abs(got[i]-c.ExpectedMs[i]) > 0.001 {
				t.Errorf("%s: action %d (%s) = %vms, want %vms", c.Name, i, c.Actions[i].Name, got[i], c.ExpectedMs[i])
			}
		}
	}
}

func TestOptionsFromPropsUsesOverrides(t *testing.T) {
	typing, standard := 30, 800
	o := OptionsFromProps(&types.CodeVideoIDEProps{KeyboardTypingPauseMs: &typing, StandardPauseMs: &standard})
	if o.KeyboardTypingPauseMs != 30 || o.StandardPauseMs != 800 {
		t.Fatalf("got %+v", o)
	}
	if d := OptionsFromProps(nil); d.KeyboardTypingPauseMs != PageKeyboardTypingPauseMs || d.StandardPauseMs != PageStandardPauseMs {
		t.Fatalf("defaults: got %+v", d)
	}
}

package elevenlabs

import "testing"

func TestTransformsApplyLongestPatternFirst(t *testing.T) {
	cases := map[string]string{
		"call console.log here": "call console dot log here",
		"save the .json file":   "save the dot jason file",
		"a JSON body":           "a jason body",
		"open the .ts file":     "open the dot T S file",
	}
	// Map order used to vary from run to run, so check many runs give the same speech.
	for input, want := range cases {
		for run := 0; run < 50; run++ {
			if got := applyCustomTransforms(input); got != want {
				t.Fatalf("applyCustomTransforms(%q) = %q, want %q", input, got, want)
			}
		}
	}
}

func TestClipKeyFollowsWhatElevenLabsIsAsked(t *testing.T) {
	key := func(text, voice string) string {
		t.Helper()
		k, err := ClipKey(text, voice)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	base := key("Open the .ts file.", "voice-a")
	if base != key("Open the .ts file.", "voice-a") {
		t.Fatal("same text and voice gave different keys")
	}
	if base == key("Open the .ts file.", "voice-b") {
		t.Fatal("a different voice gave the same key")
	}
	if base == key("Open the .js file.", "voice-a") {
		t.Fatal("different text gave the same key")
	}
	// The key is made from the text ElevenLabs receives, after the transforms.
	if base != key("Open the dot T S file.", "voice-a") {
		t.Fatal("texts that are spoken identically gave different keys")
	}
	// No voice means the default voice, so both name the same clip.
	if key("Hello.", "") != key("Hello.", "iP95p4xoKVk53GoZ742B") {
		t.Fatal("the default voice and no voice gave different keys")
	}
}

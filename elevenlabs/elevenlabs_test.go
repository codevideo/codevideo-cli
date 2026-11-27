package elevenlabs

import (
	"encoding/json"
	"testing"
)

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

func clipKey(t *testing.T, text string, voice Voice) string {
	t.Helper()
	k, err := ClipKey(text, voice)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestClipKeyFollowsWhatElevenLabsIsAsked(t *testing.T) {
	a, b := Voice{ID: "voice-a"}, Voice{ID: "voice-b"}
	base := clipKey(t, "Open the .ts file.", a)
	if base != clipKey(t, "Open the .ts file.", a) {
		t.Fatal("same text and voice gave different keys")
	}
	if base == clipKey(t, "Open the .ts file.", b) {
		t.Fatal("a different voice gave the same key")
	}
	if base == clipKey(t, "Open the .js file.", a) {
		t.Fatal("different text gave the same key")
	}
	if base == clipKey(t, "Open the .ts file.", Voice{ID: "voice-a", ModelID: "eleven_v4"}) {
		t.Fatal("a different model gave the same key")
	}
	// The key is made from the text ElevenLabs receives, after the transforms.
	if base != clipKey(t, "Open the dot T S file.", a) {
		t.Fatal("texts that are spoken identically gave different keys")
	}
	// No voice means the default voice, so both name the same clip.
	if clipKey(t, "Hello.", Voice{}) != clipKey(t, "Hello.", Voice{ID: "iP95p4xoKVk53GoZ742B"}) {
		t.Fatal("the default voice and no voice gave different keys")
	}
	// Naming the model a voice already has asks ElevenLabs the same thing.
	if clipKey(t, "Hello.", Voice{ID: "voice-a"}) != clipKey(t, "Hello.", Voice{ID: "voice-a", ModelID: "eleven_multilingual_v2"}) {
		t.Fatal("the default model named explicitly gave a different key")
	}
}

// Clips made before models could be chosen must keep their names, or every
// cached clip (local folder and S3 bucket) would be paid for again. These
// keys and bodies come from CLI 0.0.19.
func TestRequestsWithoutAModelAreUnchanged(t *testing.T) {
	cases := []struct {
		text  string
		voice Voice
		key   string
		body  string
	}{
		{
			"Hello there.", Voice{ID: "voice-a"},
			"bab26fb84238f598cfbc2d8bf6e4c485eafc577e2fb2a140484d225604ae3358",
			`{"text":"Hello there.","model_id":"eleven_multilingual_v2","voice_settings":{"stability":0.5,"similarity_boost":0.75,"style_exaggeration":0.5}}`,
		},
		{
			"Hello.", Voice{},
			"52521a576b5a6823840cbc03155cfd3b0b91c58cb723a8e847f2b3de40d96057",
			`{"text":"Hello.","model_id":"eleven_multilingual_v2","voice_settings":{"stability":0.5,"similarity_boost":0.75,"style_exaggeration":0.5}}`,
		},
		{
			"Open the .ts file.", Voice{ID: "1RLeGxy9FHYB5ScpFkts"},
			"b726234ca44e65513a9fc11c529779d60871a5785e6332c20ce353b950bbc856",
			`{"text":"Open the dot T S file.","model_id":"eleven_turbo_v2","voice_settings":{"stability":0.5,"similarity_boost":0.75,"style_exaggeration":0.5}}`,
		},
	}
	for _, c := range cases {
		if got := clipKey(t, c.text, c.voice); got != c.key {
			t.Errorf("ClipKey(%q, %+v) = %s, want %s", c.text, c.voice, got, c.key)
		}
		r, err := newRequest(c.text, c.voice)
		if err != nil {
			t.Fatal(err)
		}
		if string(r.body) != c.body {
			t.Errorf("body for %q = %s, want %s", c.text, r.body, c.body)
		}
	}
}

func sentModelAndText(t *testing.T, text string, voice Voice) (string, string) {
	t.Helper()
	r, err := newRequest(text, voice)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Text    string `json:"text"`
		ModelID string `json:"model_id"`
	}
	if err := json.Unmarshal(r.body, &body); err != nil {
		t.Fatal(err)
	}
	return body.ModelID, body.Text
}

func TestModelIsTheOneSetOrTheVoicesOwn(t *testing.T) {
	cases := []struct {
		voice Voice
		model string
	}{
		{Voice{}, "eleven_multilingual_v2"},
		{Voice{ID: "voice-a"}, "eleven_multilingual_v2"},
		{Voice{ID: "1RLeGxy9FHYB5ScpFkts"}, "eleven_turbo_v2"},
		{Voice{ID: "voice-a", ModelID: "eleven_v4"}, "eleven_v4"},
		// A model that is set wins over the clone's own model too.
		{Voice{ID: "1RLeGxy9FHYB5ScpFkts", ModelID: "eleven_v3"}, "eleven_v3"},
	}
	for _, c := range cases {
		if got := c.voice.Model(); got != c.model {
			t.Errorf("%+v.Model() = %s, want %s", c.voice, got, c.model)
		}
		if got, _ := sentModelAndText(t, "Hi.", c.voice); got != c.model {
			t.Errorf("%+v sent model_id %s, want %s", c.voice, got, c.model)
		}
	}
	r, err := newRequest("Hi.", Voice{ModelID: "eleven_v4"})
	if err != nil {
		t.Fatal(err)
	}
	if r.voiceID != "iP95p4xoKVk53GoZ742B" {
		t.Errorf("no voice with a model set went to %q, want the default voice", r.voiceID)
	}
}

func TestAudioTagsReachOnlyModelsThatActThemOut(t *testing.T) {
	const line = "[exaggerated clueless voice] uh, what is odd or even"
	for _, model := range []string{"eleven_v4", "eleven_v4_turbo", "eleven_v3", "eleven_v3_conversational"} {
		if _, text := sentModelAndText(t, line, Voice{ID: "cole", ModelID: model}); text != line {
			t.Errorf("%s was sent %q, want the tag kept", model, text)
		}
	}
	for _, model := range []string{"", "eleven_multilingual_v2", "eleven_flash_v2_5", "eleven_turbo_v2"} {
		if _, text := sentModelAndText(t, line, Voice{ID: "cole", ModelID: model}); text != "uh, what is odd or even" {
			t.Errorf("model %q was sent %q, want the tag left out", model, text)
		}
	}
}

func TestReadsAudioTags(t *testing.T) {
	cases := map[string]bool{
		"eleven_v3":                true,
		"eleven_v3_conversational": true,
		"eleven_v4":                true,
		"eleven_v4_turbo":          true,
		"eleven_v12":               true,
		"eleven_v2":                false,
		"eleven_multilingual_v2":   false,
		"eleven_flash_v2_5":        false,
		"eleven_turbo_v2":          false,
		"eleven_english_sts_v2":    false,
		"":                         false,
	}
	for model, want := range cases {
		if got := ReadsAudioTags(model); got != want {
			t.Errorf("ReadsAudioTags(%q) = %v, want %v", model, got, want)
		}
	}
}

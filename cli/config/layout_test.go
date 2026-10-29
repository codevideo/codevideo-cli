package config

import (
	"encoding/json"
	"github.com/codevideo/codevideo-cli/types"
	"os"
	"path/filepath"
	"testing"
)

func TestLayoutConfigManifestRoundTrip(t *testing.T) {
	for _, input := range []string{`{"theme":"dark"}`, `{"isFileExplorerVisible":false,"isEditorTabsVisible":false,"isMinimapVisible":false,"isMouseVisible":false,"wordWrap":"on"}`, `{"isFileExplorerVisible":true,"isEditorTabsVisible":true,"isMinimapVisible":true,"isMouseVisible":true,"wordWrap":"off"}`} {
		file := filepath.Join(t.TempDir(), "layout.json")
		if err := os.WriteFile(file, []byte(`{"theme":"dark","defaultLanguage":"python",`+input[1:]), 0600); err != nil {
			t.Fatal(err)
		}
		props, err := LoadConfigFile(file)
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := json.Marshal(types.CodeVideoManifest{CodeVideoIDEProps: props})
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Props map[string]interface{} `json:"codeVideoIDEProps"`
		}
		if err := json.Unmarshal(manifest, &got); err != nil {
			t.Fatal(err)
		}
		var expected map[string]interface{}
		if err := json.Unmarshal([]byte(input), &expected); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"isFileExplorerVisible", "isEditorTabsVisible", "isMinimapVisible", "isMouseVisible", "wordWrap"} {
			value, exists := got.Props[key]
			want, wantExists := expected[key]
			if exists != wantExists || value != want {
				t.Errorf("%s: got %v (%v), want %v (%v)", key, value, exists, want, wantExists)
			}
		}
	}
}

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTypingConfig(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		valid        bool
	}{
		{"default", "", true},
		{"humanized", `,"includeTypos":true,"keyboardTypingJitterMs":8,"keyboardTypingPauseMs":40`, true},
		{"zero", `,"includeTypos":false,"keyboardTypingJitterMs":0`, true},
		{"negative", `,"keyboardTypingJitterMs":-1`, false},
		{"fraction", `,"keyboardTypingJitterMs":0.5`, false},
		{"aboveDefault", `,"keyboardTypingJitterMs":41`, false},
		{"aboveCustom", `,"keyboardTypingJitterMs":9,"keyboardTypingPauseMs":8`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(file, []byte(`{"theme":"dark","defaultLanguage":"python"`+tc.fields+`}`), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := LoadConfigFile(file)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, err=%v", tc.valid, err)
			}
			if tc.name == "humanized" && (got.IncludeTypos == nil || !*got.IncludeTypos || *got.KeyboardTypingJitterMs != 8) {
				t.Fatalf("settings lost: %+v", got)
			}
		})
	}
}

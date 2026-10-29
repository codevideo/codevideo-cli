package types

import "testing"

func TestResolveRenderMode(t *testing.T) {
	for _, tt := range []struct {
		manifest RenderMode
		env      string
		want     RenderMode
		bad      bool
	}{
		{"", "", RenderModeRealtime, false},
		{"", "deterministic", RenderModeDeterministic, false},
		{RenderModeRealtime, "deterministic", RenderModeRealtime, false},
		{RenderModeDeterministic, "invalid", RenderModeDeterministic, false},
		{"invalid", "realtime", "", true},
		{"", "invalid", "", true},
	} {
		got, err := ResolveRenderMode(tt.manifest, tt.env)
		if got != tt.want || (err != nil) != tt.bad {
			t.Fatalf("ResolveRenderMode(%q, %q) = %q, %v", tt.manifest, tt.env, got, err)
		}
	}
}

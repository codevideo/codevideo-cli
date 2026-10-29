package types

import "fmt"

type RenderMode string

const (
	RenderModeRealtime      RenderMode = "realtime"
	RenderModeDeterministic RenderMode = "deterministic"
)

func ResolveRenderMode(manifest RenderMode, environment string) (RenderMode, error) {
	mode := manifest
	if mode == "" {
		mode = RenderMode(environment)
	}
	if mode == "" {
		mode = RenderModeRealtime
	}
	if mode != RenderModeRealtime && mode != RenderModeDeterministic {
		return "", fmt.Errorf("unknown render mode %q; use realtime or deterministic", mode)
	}
	return mode, nil
}

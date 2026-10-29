package config

import (
	"path/filepath"
	"testing"

	"github.com/codevideo/codevideo-cli/types"
	"github.com/spf13/cobra"
)

func TestRenderFlags(t *testing.T) {
	for _, tt := range []struct {
		name, env, mode, strategy string
		workers                   int
		want                      types.RenderMode
		bad                       bool
	}{
		{"default", "", "", "sequential-seek", 1, types.RenderModeRealtime, false},
		{"environment", "deterministic", "", "ab-initio", 4, types.RenderModeDeterministic, false},
		{"explicit flag wins", "deterministic", "realtime", "sequential-seek", 1, types.RenderModeRealtime, false},
		{"unknown mode", "", "parallel", "sequential-seek", 1, "", true},
		{"zero workers", "", "deterministic", "sequential-seek", 0, "", true},
		{"too many workers", "", "deterministic", "sequential-seek", 33, "", true},
		{"legacy workers", "", "realtime", "sequential-seek", 4, "", true},
		{"unknown strategy", "", "deterministic", "unknown", 1, "", true},
		{"checkpoint realtime", "", "realtime", "ab-initio", 1, "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("CODEVIDEO_WORK_DIR", dir)
			t.Setenv("CODEVIDEO_RENDER_MODE", tt.env)
			old := GlobalConfig
			GlobalConfig = DefaultConfig()
			defer func() { GlobalConfig = old }()
			cmd := &cobra.Command{}
			f := cmd.Flags()
			f.String("render-mode", "realtime", "")
			f.String("render-seed", "seed", "")
			f.String("render-strategy", tt.strategy, "")
			f.Int("render-workers", tt.workers, "")
			f.String("output", filepath.Join(dir, "test.mp4"), "")
			if tt.mode != "" {
				if err := f.Set("render-mode", tt.mode); err != nil {
					t.Fatal(err)
				}
			}
			err := LoadFromFlags(cmd)
			if (err != nil) != tt.bad {
				t.Fatalf("unexpected result: %v", err)
			}
			if !tt.bad && (GlobalConfig.RenderMode != tt.want || GlobalConfig.RenderSeed != "seed" || GlobalConfig.RenderWorkers != tt.workers || string(GlobalConfig.RenderStrategy) != tt.strategy) {
				t.Fatalf("wrong resolved config: %+v", GlobalConfig)
			}
		})
	}
}

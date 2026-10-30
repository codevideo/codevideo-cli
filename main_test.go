package main

import (
	"bytes"
	"strings"
	"testing"

	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/codevideo/codevideo-cli/cli/config"
)

// command parses args against the real flag set without running anything.
func command(t *testing.T, args ...string) (*cobra.Command, *bytes.Buffer, error) {
	t.Helper()
	var stderr bytes.Buffer
	cmd := &cobra.Command{Use: "codevideo", Run: func(*cobra.Command, []string) {}}
	addFlags(cmd)
	cmd.SetArgs(args)
	cmd.SetErr(&stderr)
	cmd.Flags().SetOutput(&stderr)
	return cmd, &stderr, cmd.Execute()
}

func TestConsoleLevel(t *testing.T) {
	for _, tt := range []struct {
		args  []string
		serve bool
		want  log.Level
	}{
		{nil, false, log.WarnLevel},
		{[]string{"-v"}, false, log.InfoLevel},
		{[]string{"--verbose"}, false, log.InfoLevel},
		{[]string{"-q"}, false, log.ErrorLevel},
		{[]string{"--log-level", "debug"}, false, log.DebugLevel},
		{[]string{"-v", "--log-level", "trace"}, false, log.TraceLevel},
		{[]string{"-q", "--log-level", "info"}, false, log.InfoLevel},
		{nil, true, log.DebugLevel},
		{[]string{"--log-level", "info"}, true, log.InfoLevel},
	} {
		cmd, _, err := command(t, tt.args...)
		if err != nil {
			t.Fatalf("%v: %v", tt.args, err)
		}
		got, err := consoleLevel(cmd, tt.serve)
		if err != nil || got != tt.want {
			t.Errorf("consoleLevel(%v, serve=%v) = %v, %v; want %v", tt.args, tt.serve, got, err, tt.want)
		}
	}
	cmd, _, _ := command(t, "--log-level", "loud")
	if _, err := consoleLevel(cmd, false); err == nil || !strings.Contains(err.Error(), "loud") {
		t.Errorf("bad --log-level: %v", err)
	}
}

func TestVerboseAndQuietConflict(t *testing.T) {
	if _, _, err := command(t, "-v", "-q"); err == nil {
		t.Fatal("-v with -q was accepted")
	}
}

func TestDebugFlagIsADeprecatedHeaded(t *testing.T) {
	t.Setenv("CODEVIDEO_WORK_DIR", t.TempDir())
	previous := config.GlobalConfig
	t.Cleanup(func() { config.GlobalConfig = previous })
	for _, args := range [][]string{{"--headed"}, {"--debug"}, {"-d"}} {
		config.GlobalConfig = config.DefaultConfig()
		cmd, stderr, err := command(t, args...)
		if err != nil {
			t.Fatal(err)
		}
		if err := config.LoadFromFlags(cmd); err != nil {
			t.Fatal(err)
		}
		if !config.GlobalConfig.Headed {
			t.Errorf("%v did not set Headed", args)
		}
		if deprecated := strings.Contains(stderr.String(), "use --headed instead"); deprecated != (args[0] != "--headed") {
			t.Errorf("%v: stderr = %q", args, stderr)
		}
	}
}

func TestConsoleLine(t *testing.T) {
	for _, tt := range []struct {
		level   log.Level
		message string
		want    string
	}{
		{log.InfoLevel, "Loaded config from: portrait.json\n", "Loaded config from: portrait.json"},
		{log.WarnLevel, "Failed to move manifest", "warning: Failed to move manifest"},
		{log.ErrorLevel, "job failed", "error: job failed"},
		{log.DebugLevel, "ffmpeg: 1.0s", "debug: ffmpeg: 1.0s"},
	} {
		if got := consoleLine(tt.level, tt.message); got != tt.want {
			t.Errorf("consoleLine(%v, %q) = %q, want %q", tt.level, tt.message, got, tt.want)
		}
	}
	hook := &consoleHook{level: log.WarnLevel}
	if got := hook.Levels(); len(got) != 4 || got[3] != log.WarnLevel {
		t.Errorf("warn hook levels = %v", got)
	}
}

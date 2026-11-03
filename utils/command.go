package utils

import (
	"context"
	"os"
	"os/exec"
	"time"
)

// StopDelay is how long a cancelled command gets to exit after the interrupt
// before it is killed.
const StopDelay = 10 * time.Second

// CommandContext is exec.CommandContext for the long-running programs a
// render starts: the Node runners and ffmpeg. Cancelling ctx interrupts the
// program instead of killing it, so a runner can close Chrome and ffmpeg can
// finish its file; a program still running StopDelay later is killed.
// Windows has no interrupt to send, so there the program is killed at once.
func CommandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Cancel = func() error {
		if err := cmd.Process.Signal(os.Interrupt); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
	cmd.WaitDelay = StopDelay
	return cmd
}

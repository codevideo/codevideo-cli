package utils

import (
	"fmt"
	"os/exec"
	"runtime"
)

func OpenFile(filepath string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin": // macOS
		cmd = exec.Command("open", filepath)
	case "linux":
		cmd = exec.Command("xdg-open", filepath)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", filepath)
	default:
		return fmt.Errorf("unsupported platform")
	}

	return cmd.Start()
}

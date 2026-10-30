//go:build windows

package renderer

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableVT turns on escape sequence handling in the Windows console, which
// the repaint needs. Consoles without it (before Windows 10) get plain lines.
func enableVT(f *os.File) bool {
	handle := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(handle, &mode); err != nil {
		return false
	}
	return windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}

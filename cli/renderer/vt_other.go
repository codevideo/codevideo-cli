//go:build !windows

package renderer

import "os"

// enableVT reports whether the terminal handles escape sequences; Unix
// terminals do.
func enableVT(*os.File) bool { return true }

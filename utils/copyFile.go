package utils

import (
	"io"
	"os"
)

// CopyFile copies the contents and permissions from src to dst.
func CopyFile(src, dst string) error {
	// Open the source file
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Create the destination file
	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	// Copy the file contents from source to destination
	if _, err = io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	// Copy file permissions from source to destination
	sourceInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, sourceInfo.Mode())
}

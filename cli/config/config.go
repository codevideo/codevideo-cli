package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Config holds all configuration settings for the CLI
type Config struct {
	// Project settings
	ProjectJSON string

	// Output settings
	OutputDir      string
	OutputFileName string
	OutputFormat   string

	// Processing settings
	MaxConcurrentJobs int
	Resolution        string

	// Environment
	OperatingSystem string
	Environment     string
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		OutputDir:         ".",
		OutputFileName:    fmt.Sprintf("CodeVideo-%s", time.Now().Format("2006-01-02-15-04-05")),
		OutputFormat:      "mp4",
		MaxConcurrentJobs: runtime.NumCPU() / 2, // Use half of available CPUs
		Resolution:        "1080p",              // 1080p by default, could be 4K
		OperatingSystem:   runtime.GOOS,
		Environment:       "local",
	}
}

// EnsureOutputDirs ensures that all required output directories exist
func (c *Config) EnsureOutputDirs() error {
	// Make sure output directory exists
	if err := os.MkdirAll(c.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create temp directories for processing
	tempDirs := []string{
		filepath.Join(os.TempDir(), "codevideo", "new"),
		filepath.Join(os.TempDir(), "codevideo", "error"),
		filepath.Join(os.TempDir(), "codevideo", "success"),
		filepath.Join(os.TempDir(), "codevideo", "video"),
	}

	for _, dir := range tempDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create temp directory %s: %w", dir, err)
		}
	}

	return nil
}

// GetTempDir returns the path to a specific temporary directory
func (c *Config) GetTempDir(dirType string) string {
	return filepath.Join(os.TempDir(), "codevideo", dirType)
}

// GenerateOutputPath generates the full path for an output file
func (c *Config) GenerateOutputPath(suffix string) string {
	filename := c.OutputFileName
	if suffix != "" {
		filename = fmt.Sprintf("%s-%s", filename, suffix)
	}
	return filepath.Join(c.OutputDir, fmt.Sprintf("%s.%s", filename, c.OutputFormat))
}

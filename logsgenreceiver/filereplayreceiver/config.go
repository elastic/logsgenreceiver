package filereplayreceiver

import (
	"errors"
	"fmt"
	"path/filepath"

	"go.opentelemetry.io/collector/component"
)

type Config struct {
	// Path is a file path or glob pattern, e.g. "/data/*.jsonl.zst"
	Path string `mapstructure:"path"`
	// Workers is the number of parallel ConsumeLogs goroutines (default: 1)
	Workers int `mapstructure:"workers"`
	// ScanBufferBytes is the bufio.Scanner token buffer size (default: 16MB)
	ScanBufferBytes int `mapstructure:"scan_buffer_bytes"`
}

func (c *Config) Validate() error {
	if c.Path == "" {
		return errors.New("path is required")
	}
	if _, err := filepath.Glob(c.Path); err != nil {
		return fmt.Errorf("invalid path/glob %q: %w", c.Path, err)
	}
	if c.Workers < 0 {
		return errors.New("workers must be >= 0")
	}
	return nil
}

func createDefaultConfig() component.Config {
	return &Config{
		Workers:         1,
		ScanBufferBytes: 16 * 1024 * 1024, // 16MB
	}
}

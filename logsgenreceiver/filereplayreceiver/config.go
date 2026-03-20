package filereplayreceiver

import (
	"errors"
	"fmt"
	"path/filepath"

	"go.opentelemetry.io/collector/component"
)

// SplitBy controls how each line of the input file is split before being
// handed to the downstream consumer.
type SplitBy string

const (
	// SplitByLine emits one plog.Logs per file line (original behavior).
	// Use this when the file contains small, already-normalized documents.
	SplitByLine SplitBy = "line"

	// SplitByResourceLogs emits one plog.Logs per ResourceLogs entry within
	// each line. Recommended for pre-batched OTLP datasets (e.g. Filebeat
	// exports) where each line bundles many sources into a single document.
	SplitByResourceLogs SplitBy = "resource_logs"

	// SplitByLogRecord emits one plog.Logs per individual log record,
	// preserving its resource and scope context. Use to simulate agent-level
	// single-event traffic.
	SplitByLogRecord SplitBy = "log_record"
)

type Config struct {
	// Path is a file path or glob pattern, e.g. "/data/*.jsonl.zst"
	Path string `mapstructure:"path"`
	// Workers is the number of parallel ConsumeLogs goroutines (default: 1)
	Workers int `mapstructure:"workers"`
	// ScanBufferBytes is the bufio.Scanner token buffer size (default: 16MB)
	ScanBufferBytes int `mapstructure:"scan_buffer_bytes"`
	// SplitBy controls how each parsed line is split before being forwarded.
	// Valid values: "line", "resource_logs", "log_record" (default: "line")
	SplitBy SplitBy `mapstructure:"split_by"`
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
	switch c.SplitBy {
	case SplitByLine, SplitByResourceLogs, SplitByLogRecord:
	default:
		return fmt.Errorf("invalid split_by %q: must be one of \"line\", \"resource_logs\", \"log_record\"", c.SplitBy)
	}
	return nil
}

func createDefaultConfig() component.Config {
	return &Config{
		Workers:         1,
		ScanBufferBytes: 16 * 1024 * 1024, // 16MB
		SplitBy:         SplitByLine,
	}
}

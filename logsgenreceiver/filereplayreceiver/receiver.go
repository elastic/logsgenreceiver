package filereplayreceiver

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/klauspost/compress/zstd"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componentstatus"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/receiver"
	"go.uber.org/zap"
)

type fileReplayReceiver struct {
	cfg      *Config
	settings receiver.Settings
	nextLogs consumer.Logs
	host     component.Host
	cancel   context.CancelFunc
	done     chan struct{}
}

func (r *fileReplayReceiver) Start(ctx context.Context, host component.Host) error {
	r.host = host
	ctx, r.cancel = context.WithCancel(ctx)
	go r.run(ctx)
	return nil
}

func (r *fileReplayReceiver) Shutdown(_ context.Context) error {
	if r.cancel != nil {
		r.cancel()
	}
	<-r.done
	return nil
}

func (r *fileReplayReceiver) run(ctx context.Context) {
	defer close(r.done)

	files, err := filepath.Glob(r.cfg.Path)
	if err != nil || len(files) == 0 {
		r.settings.Logger.Error("no files matched", zap.String("path", r.cfg.Path))
		r.reportDone()
		return
	}

	workers := r.cfg.Workers
	if workers <= 1 {
		r.runSingle(ctx, files)
	} else {
		r.runParallel(ctx, files, workers)
	}

	if ctx.Err() == nil {
		r.reportDone()
	}
}

// runSingle: tight loop, no channel
func (r *fileReplayReceiver) runSingle(ctx context.Context, files []string) {
	unmarshaler := plog.JSONUnmarshaler{}
	for _, path := range files {
		if ctx.Err() != nil {
			return
		}
		if err := r.readFile(ctx, path, func(line []byte) {
			logs, err := unmarshaler.UnmarshalLogs(line)
			if err != nil {
				r.settings.Logger.Warn("unmarshal error", zap.Error(err))
				return
			}
			if err := r.nextLogs.ConsumeLogs(ctx, logs); err != nil {
				r.settings.Logger.Warn("consume error", zap.Error(err))
			}
		}); err != nil {
			r.settings.Logger.Error("file read error", zap.String("path", path), zap.Error(err))
		}
	}
}

// runParallel: reader → channel → N workers
func (r *fileReplayReceiver) runParallel(ctx context.Context, files []string, n int) {
	ch := make(chan []byte, 256)
	unmarshaler := plog.JSONUnmarshaler{}
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for line := range ch {
				logs, err := unmarshaler.UnmarshalLogs(line)
				if err != nil {
					r.settings.Logger.Warn("unmarshal error", zap.Error(err))
					continue
				}
				if err := r.nextLogs.ConsumeLogs(ctx, logs); err != nil {
					r.settings.Logger.Warn("consume error", zap.Error(err))
				}
			}
		}()
	}

	for _, path := range files {
		if ctx.Err() != nil {
			break
		}
		_ = r.readFile(ctx, path, func(line []byte) {
			buf := make([]byte, len(line))
			copy(buf, line)
			ch <- buf
		})
	}
	close(ch)
	wg.Wait()
}

// readFile opens a file (decompressing .zst if needed), scans lines, calls fn for each
func (r *fileReplayReceiver) readFile(ctx context.Context, path string, fn func([]byte)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var rd io.Reader = f
	if strings.HasSuffix(path, ".zst") {
		zr, err := zstd.NewReader(f)
		if err != nil {
			return err
		}
		defer zr.Close()
		rd = zr
	}

	scanner := bufio.NewScanner(rd)
	buf := make([]byte, r.cfg.ScanBufferBytes)
	scanner.Buffer(buf, r.cfg.ScanBufferBytes)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}
		fn(scanner.Bytes())
	}
	return scanner.Err()
}

func (r *fileReplayReceiver) reportDone() {
	componentstatus.ReportStatus(
		r.host,
		componentstatus.NewFatalErrorEvent(errors.New("filereplay: finished reading all files")),
	)
}

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
	"sync/atomic"
	"time"

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

	// progress counters — updated atomically on the hot path
	linesRead atomic.Uint64
	logsRead  atomic.Uint64
	startTime time.Time
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

	r.startTime = time.Now()
	go r.logProgress(ctx)

	workers := r.cfg.Workers
	if workers <= 1 {
		r.runSingle(ctx, files)
	} else {
		r.runParallel(ctx, files, workers)
	}

	if ctx.Err() == nil {
		r.logSummary()
		r.reportDone()
	}
}

// logProgress emits a progress line every 10 s until ctx is cancelled.
func (r *fileReplayReceiver) logProgress(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			elapsed := time.Since(r.startTime).Seconds()
			logs := r.logsRead.Load()
			r.settings.Logger.Info("filereplay progress",
				zap.Uint64("lines_read", r.linesRead.Load()),
				zap.Uint64("logs_read", logs),
				zap.Float64("logs_per_second", float64(logs)/elapsed),
				zap.String("elapsed", time.Since(r.startTime).Round(time.Millisecond).String()),
			)
		}
	}
}

func (r *fileReplayReceiver) logSummary() {
	elapsed := time.Since(r.startTime)
	logs := r.logsRead.Load()
	r.settings.Logger.Info("filereplay finished",
		zap.Uint64("lines_read", r.linesRead.Load()),
		zap.Uint64("logs_read", logs),
		zap.Float64("logs_per_second", float64(logs)/elapsed.Seconds()),
		zap.String("duration", elapsed.Round(time.Millisecond).String()),
	)
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
			r.emit(ctx, logs)
			r.linesRead.Add(1)
			r.logsRead.Add(uint64(logs.LogRecordCount()))
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
				r.emit(ctx, logs)
				r.linesRead.Add(1)
				r.logsRead.Add(uint64(logs.LogRecordCount()))
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

// emit forwards logs to the next consumer, splitting according to cfg.SplitBy.
func (r *fileReplayReceiver) emit(ctx context.Context, logs plog.Logs) {
	switch r.cfg.SplitBy {
	case SplitByResourceLogs:
		rls := logs.ResourceLogs()
		for i := 0; i < rls.Len(); i++ {
			slice := plog.NewLogs()
			rls.At(i).CopyTo(slice.ResourceLogs().AppendEmpty())
			if err := r.nextLogs.ConsumeLogs(ctx, slice); err != nil {
				r.settings.Logger.Warn("consume error", zap.Error(err))
			}
		}
	case SplitByLogRecord:
		rls := logs.ResourceLogs()
		for i := 0; i < rls.Len(); i++ {
			rl := rls.At(i)
			sls := rl.ScopeLogs()
			for j := 0; j < sls.Len(); j++ {
				sl := sls.At(j)
				lrs := sl.LogRecords()
				for k := 0; k < lrs.Len(); k++ {
					slice := plog.NewLogs()
					newRL := slice.ResourceLogs().AppendEmpty()
					rl.Resource().CopyTo(newRL.Resource())
					newSL := newRL.ScopeLogs().AppendEmpty()
					sl.Scope().CopyTo(newSL.Scope())
					lrs.At(k).CopyTo(newSL.LogRecords().AppendEmpty())
					if err := r.nextLogs.ConsumeLogs(ctx, slice); err != nil {
						r.settings.Logger.Warn("consume error", zap.Error(err))
					}
				}
			}
		}
	default: // SplitByLine
		if err := r.nextLogs.ConsumeLogs(ctx, logs); err != nil {
			r.settings.Logger.Warn("consume error", zap.Error(err))
		}
	}
}

func (r *fileReplayReceiver) reportDone() {
	componentstatus.ReportStatus(
		r.host,
		componentstatus.NewFatalErrorEvent(errors.New("filereplay: finished reading all files")),
	)
}

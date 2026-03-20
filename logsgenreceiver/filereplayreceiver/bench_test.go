package filereplayreceiver

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/elastic/logsgenreceiver/logsgenreceiver/filereplayreceiver/internal/metadata"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/receiver/receivertest"
)

// Matches k8s-multiapp-filebeat-logs-v4-1000000.jsonl:
//
//	1,000 lines × 1,000 log records per line = 1,000,000 total
//	40 ResourceLogs per line, 25 LogRecords each
const (
	benchLines        = 1000
	benchLogsPerLine  = 1000
	benchResources    = 40
	benchRecsPerRes   = benchLogsPerLine / benchResources // 25
	benchWorkers      = 4
	benchScanBufBytes = 32 * 1024 * 1024 // 32 MB
)

var benchFilePath string

func TestMain(m *testing.M) {
	f, err := os.CreateTemp("", "filereplay-bench-*.jsonl")
	if err != nil {
		panic(fmt.Sprintf("create temp file: %v", err))
	}
	benchFilePath = f.Name()
	f.Close()

	if err := generateBenchFile(benchFilePath); err != nil {
		panic(fmt.Sprintf("generate bench file: %v", err))
	}

	info, _ := os.Stat(benchFilePath)
	fmt.Printf("bench file: %s  lines=%d  logs=%d  size=%.1f MB\n",
		benchFilePath, benchLines, benchLines*benchLogsPerLine,
		float64(info.Size())/1024/1024)

	code := m.Run()
	os.Remove(benchFilePath)
	os.Exit(code)
}

// nopSink counts consumed log records without doing any work.
type nopSink struct{ count atomic.Uint64 }

func (s *nopSink) ConsumeLogs(_ context.Context, ld plog.Logs) error {
	s.count.Add(uint64(ld.LogRecordCount()))
	return nil
}

func (s *nopSink) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

// generateBenchFile writes benchLines NDJSON lines to path.
// Structure mirrors k8s-multiapp-filebeat-logs-v4-1000000.jsonl:
// 40 ResourceLogs per line, 25 LogRecords each, k8s + HTTP attributes.
func generateBenchFile(path string) error {
	marshaler := plog.JSONMarshaler{}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	bw := bufio.NewWriterSize(f, 8*1024*1024)
	for i := 0; i < benchLines; i++ {
		data, err := marshaler.MarshalLogs(makeBatch(i))
		if err != nil {
			return fmt.Errorf("line %d: %w", i, err)
		}
		if _, err := bw.Write(data); err != nil {
			return err
		}
		if err := bw.WriteByte('\n'); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// makeBatch returns a plog.Logs with benchResources ResourceLogs × benchRecsPerRes
// LogRecords each (= benchLogsPerLine total), with k8s-style attributes.
func makeBatch(batchIdx int) plog.Logs {
	severities := []plog.SeverityNumber{
		plog.SeverityNumberInfo, plog.SeverityNumberInfo, plog.SeverityNumberInfo,
		plog.SeverityNumberWarn, plog.SeverityNumberError, plog.SeverityNumberDebug,
	}
	severityTexts := []string{"INFO", "INFO", "INFO", "WARN", "ERROR", "DEBUG"}
	methods := []string{"GET", "GET", "GET", "POST", "PUT", "DELETE"}
	services := []string{
		"nginx", "api-gateway", "user-service", "order-service",
		"payment-service", "proxy", "mysql", "redis",
	}

	ld := plog.NewLogs()
	for r := 0; r < benchResources; r++ {
		svc := services[r%len(services)]
		node := fmt.Sprintf("ip-10-0-%d-0.eu-west-1.compute.internal", r%10)
		podUID := fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
			batchIdx+1, r+1, batchIdx*r+1, r, batchIdx*benchResources+r)

		rl := ld.ResourceLogs().AppendEmpty()
		res := rl.Resource()
		res.Attributes().PutStr("service.name", svc)
		res.Attributes().PutStr("k8s.cluster.name", "benchmark-cluster-eu-west-1")
		res.Attributes().PutStr("k8s.namespace.name", fmt.Sprintf("ns-%d", r%5))
		res.Attributes().PutStr("k8s.pod.name", fmt.Sprintf("%s-%s-%d", svc, podUID[:8], r))
		res.Attributes().PutStr("k8s.pod.uid", podUID)
		res.Attributes().PutStr("k8s.node.name", node)
		res.Attributes().PutStr("k8s.node.uid", fmt.Sprintf("%08x-node-%04d", batchIdx, r%10))
		res.Attributes().PutStr("k8s.container.name", svc)
		res.Attributes().PutStr("k8s.deployment.name", svc)
		res.Attributes().PutStr("container.id", fmt.Sprintf("%064x", batchIdx*benchResources+r))
		res.Attributes().PutStr("container.image.name", "docker.io/elastic/"+svc)
		res.Attributes().PutStr("container.image.tag", "1.2.3")
		res.Attributes().PutStr("host.name", node)
		res.Attributes().PutStr("host.id", fmt.Sprintf("i-%016x", r%10))
		res.Attributes().PutStr("cloud.provider", "aws")
		res.Attributes().PutStr("cloud.region", "eu-west-1")
		res.Attributes().PutStr("cloud.availability_zone", fmt.Sprintf("eu-west-1%c", 'a'+r%3))
		res.Attributes().PutStr("cloud.account.id", fmt.Sprintf("123456789%03d", r%3))

		sl := rl.ScopeLogs().AppendEmpty()
		sl.Scope().SetName("benchmark-scope")

		for j := 0; j < benchRecsPerRes; j++ {
			absIdx := batchIdx*benchLogsPerLine + r*benchRecsPerRes + j
			sev := severities[absIdx%len(severities)]
			method := methods[absIdx%len(methods)]
			status := int64(200)
			if sev == plog.SeverityNumberError {
				status = 500
			} else if sev == plog.SeverityNumberWarn {
				status = 429
			}

			lr := sl.LogRecords().AppendEmpty()
			lr.SetTimestamp(pcommon.Timestamp(uint64(1_735_689_600+absIdx) * 1e9))
			lr.SetSeverityNumber(sev)
			lr.SetSeverityText(severityTexts[absIdx%len(severityTexts)])
			lr.Body().SetStr(fmt.Sprintf(
				`%s /api/v1/resource/%d HTTP/1.1 %d %d `+
					`rt=%.6f uct=%.6f uht=%.6f urt=%.6f `+
					`pid=%d tid=%d`,
				method, j%500, status, 128+j%4096,
				float64(absIdx%1000)*0.001, float64(j%100)*0.0001,
				float64(j%200)*0.0005, float64(j%500)*0.001,
				absIdx%65535, j%1024,
			))

			lr.Attributes().PutStr("http.method", method)
			lr.Attributes().PutStr("http.url", fmt.Sprintf("/api/v1/resource/%d", j%500))
			lr.Attributes().PutStr("http.flavor", "1.1")
			lr.Attributes().PutInt("http.status_code", status)
			lr.Attributes().PutInt("http.request.body.size", int64(64+j%2048))
			lr.Attributes().PutInt("http.response.body.size", int64(128+j%4096))
			lr.Attributes().PutStr("net.peer.ip", fmt.Sprintf("10.%d.%d.%d", r%255, j%255, (r+j)%255))
			lr.Attributes().PutStr("net.host.name", node)
			lr.Attributes().PutStr("user_agent.original", "k8s-bench/1.0 (+http://benchmark.elastic.co)")
			lr.Attributes().PutStr("k8s.pod.name", fmt.Sprintf("%s-%s-%d", svc, podUID[:8], r))
			lr.Attributes().PutStr("service.version", "1.2.3")
			lr.Attributes().PutDouble("process.cpu_seconds_total", float64(absIdx)*0.001)
			lr.Attributes().PutInt("process.uptime_seconds", int64(absIdx%86400))
			if sev == plog.SeverityNumberError {
				lr.Attributes().PutStr("error.message", fmt.Sprintf(
					"upstream connect error or disconnect/reset before headers. reset reason: connection timeout (attempt %d)", j))
				lr.Attributes().PutStr("error.type", "timeout")
			}
		}
	}
	return ld
}

func BenchmarkFileReplayReceiver(b *testing.B) {
	b.Run(fmt.Sprintf("workers=%d", benchWorkers), func(b *testing.B) {
		runBench(b, benchWorkers)
	})
}

func runBench(b *testing.B, workers int) {
	b.Helper()
	factory := NewFactory()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()
		sink := &nopSink{}
		cfg := &Config{
			Path:            benchFilePath,
			Workers:         workers,
			ScanBufferBytes: benchScanBufBytes,
		}
		rcv, err := factory.CreateLogs(
			context.Background(),
			receivertest.NewNopSettings(metadata.Type),
			cfg,
			sink,
		)
		if err != nil {
			b.Fatal(err)
		}
		host := componenttest.NewNopHost()

		b.StartTimer()
		start := time.Now()
		if err := rcv.Start(context.Background(), host); err != nil {
			b.Fatal(err)
		}
		<-rcv.(*fileReplayReceiver).done
		elapsed := time.Since(start)
		b.StopTimer()

		if err := rcv.Shutdown(context.Background()); err != nil {
			b.Fatal(err)
		}

		total := sink.count.Load()
		b.ReportMetric(float64(total)/elapsed.Seconds(), "logs/sec")
		b.ReportMetric(float64(total), "logs")
	}
}

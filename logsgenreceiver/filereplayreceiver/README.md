# File Replay Receiver

| Status    |                   |
|-----------|-------------------|
| Stability | development: logs |

Reads OTLP JSON NDJSON files and pushes each batch directly into the collector pipeline as fast as possible. Designed for benchmarking log ingest pipelines by eliminating HTTP marshaling and network-stack overhead.

Each line in the input file must be a JSON-encoded OTLP `ExportLogsServiceRequest` (the format produced by the [File Exporter](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/exporter/fileexporter)). When all files have been consumed, the receiver signals the collector to shut down.

## Configuration

```yaml
receivers:
  filereplay:
    path: /data/logs.jsonl.zst
    workers: 4
    scan_buffer_bytes: 16777216
```

| Setting            | Default    | Description                                                                                         |
|--------------------|------------|-----------------------------------------------------------------------------------------------------|
| `path`             | _(required)_ | File path or glob pattern, e.g. `/data/*.jsonl` or `/data/*.jsonl.zst`.                           |
| `workers`          | `1`        | Number of parallel `ConsumeLogs` goroutines. `1` uses a tight loop with no channel overhead.        |
| `scan_buffer_bytes`| `16777216` | `bufio.Scanner` token buffer size (bytes). Increase if individual OTLP batches exceed 16 MB.        |

### Compression

Files ending in `.zst` are automatically decompressed using `zstd`. No configuration required.

### Glob patterns

Standard Go [`filepath.Glob`](https://pkg.go.dev/path/filepath#Glob) patterns are supported. Files are processed in the order returned by the glob.

### Workers

- **`workers: 1`** (default) — single tight loop: reader and consumer share the same goroutine, zero channel overhead. Best for single-core or I/O-bound workloads.
- **`workers: N`** — one reader goroutine feeds a buffered channel (depth 256); N goroutines unmarshal and call `ConsumeLogs` in parallel. Use when the downstream exporter or the JSON unmarshaling is the bottleneck.

## Shutdown behaviour

Once all matched files are fully consumed, the receiver calls `componentstatus.NewFatalErrorEvent` to trigger a clean collector shutdown. This makes it suitable for one-shot benchmark runs without requiring manual termination.

If the receiver is shut down externally (e.g. SIGTERM) before EOF, it drains in-flight work and exits cleanly.

## Example: benchmark with nop exporter

```yaml
receivers:
  filereplay:
    path: /data/k8s-multiapp-logs-1000000.jsonl.zst
    workers: 4

processors:
  batch:

exporters:
  nop:

service:
  pipelines:
    logs:
      receivers: [filereplay]
      processors: [batch]
      exporters: [nop]
```

## Example: generate then replay

**Step 1** — generate a dataset with the `logsgen` receiver and write to file:

```yaml
receivers:
  logsgen:
    start_time: "2025-01-01T00:00:00Z"
    end_time:   "2025-01-01T01:00:00Z"
    interval: 10s
    exit_after_end: true
    seed: 42
    scenarios:
      - path: builtin/k8s-nginx
        scale: 50
        logs_per_interval: 100

exporters:
  file:
    path: /data/nginx-logs.jsonl

service:
  pipelines:
    logs:
      receivers: [logsgen]
      exporters: [file]
```

**Step 2** — replay the file into your target exporter:

```yaml
receivers:
  filereplay:
    path: /data/nginx-logs.jsonl
    workers: 2

exporters:
  elasticsearch:
    endpoint: "https://localhost:9200"
    ...

service:
  pipelines:
    logs:
      receivers: [filereplay]
      exporters: [elasticsearch]
```

## Comparison with `otlpjsonfilereceiver`

The contrib [`otlpjsonfilereceiver`](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/otlpjsonfilereceiver) reads the same file format but is poll-driven (200 ms default interval) and has no throughput-maximizing mode. `filereplayreceiver` runs as a tight loop with no artificial delays, making it suitable for saturating a pipeline in benchmark conditions.

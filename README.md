# Log generation receiver

| Status        |                          |
| ------------- |--------------------------|
| Stability     | development: logs        |

Generates synthetic log data with configurable profiles, scale, and volume shaping.
Designed for benchmarking log ingest pipelines with realistic, production-calibrated log patterns.

Given a set of log profile configurations, this receiver generates logs with a configurable scale, start time, end time, and interval.
It simulates multiple services (nginx, mysql, redis, go microservices, HTTP proxy) running on a Kubernetes cluster,
producing log records with realistic severity distributions, field cardinality, and volume patterns.

## Getting Started

### Quick Start

To get started quickly, [check out the releases page](https://github.com/elastic/logsgenreceiver/releases) for compiled binaries you can use to get started right away. 

### Building

logsgenreceiver is a receiver for the otel collector. To build the otelcollector, the tool
[ocb](https://opentelemetry.io/docs/collector/custom-collector/) is needed. To install it on OS X, run

```bash
curl --proto '=https' --tlsv1.2 -fL -o ocb \
https://github.com/open-telemetry/opentelemetry-collector-releases/releases/download/cmd%2Fbuilder%2Fv0.139.0/ocb_0.139.0_darwin_arm64
chmod +x ocb
```

Be aware, currently this exact version is needed (v0.139.0).

Then run the following command:

```bash
./ocb --config builder-config.yaml
```

This will build the otel collector binary under `./otelcol-dev/otelcol`.

You can start the Otel Collector with the predefined config:

```
./otelcol-dev/otelcol --config otelcol-logs-medium.yaml
```

For local development, you can use the `otelcol.dev.yaml` config file which is ignored by default.


### Receiver Settings

It is possible to adjust the logsgen receiver with the configs listed below to enable different scenarios. To do this, adjust the section below:

```
receivers:
  logsgen:
    ...
```

* `start_time`: the start time of the generated logs (inclusive).
* `start_now_minus`: the duration to subtract from the current time to set the start time.
  Note that when using this option, the data generation will not be deterministic.
* `end_time`: the end time of the generated logs (exclusive).
* `end_now_minus`: the duration to subtract from the current time to set the end time.
  Note that when using this option, the data generation will not be deterministic.
* `interval`: the interval at which the logs are generated.
  The minimum value is 1s.
* `interval_jitter_std_dev` (default `0`): when set to a non-zero value (such as `5ms`), a jitter is added to the interval.
  The jitter is equal to the absolute value of a normal distribution with a median of 0ms and a standard deviation of the specified value.
  It is capped by the interval, so that the next interval will never start at or before the previous one.
  This simulates real-world scenarios where the collection interval is sometimes a bit late.
* `real_time` (default `false`): by default, the receiver generates the logs as fast as possible.
  When set to true, it will pause after each cycle according to the configured `interval`.
* `exit_after_end` (default `false`): when set to true, will terminate the collector.
* `exit_after_end_timeout` (default `0`): timeout in case exit_after_end is set to true before the collector is terminated.
* `seed` (default `0`): seed value for the random number generator. The seed makes sure that the data generation is deterministic.
* `scenarios`: a list of log generation scenarios. Each scenario defines resource attributes (via templates) and log message generation.
  * `path`: the path of the log scenario. Use `builtin/<name>` for built-in templates, or a filesystem path for custom templates.
  * `scale`: total number of simulated resource instances (e.g. pods, containers).
  * `logs_per_interval`: number of log records to generate per instance per interval.
  * `concurrency` (default `0`): when non-zero, simulates instances concurrently.
  * `template_vars`: variables for template rendering (e.g. `nodes`, `pods_per_node` for k8s topology).
  * `emit_trace_context`, `severity_weights`, `ip_pool`, `instance_volume_skew`, `needles`, `volume_profile`, `diurnal_profile`: see the [Log Generation Tuning Guide](docs/log-tuning-guide.md) for detailed documentation, parameter tables, worked examples, and ready-to-use presets.
  * Built-in log scenarios: `builtin/k8s-nginx`, `builtin/k8s-mysql`, `builtin/k8s-redis`, `builtin/k8s-goapp`, `builtin/k8s-proxy`.

### Adding new log types

To add a new log type:

1. **Create a resource-attributes template**: Create `<path>-resource-attributes.yaml` (or `.json`) in OTLP logs format. This defines the resource attributes (e.g. `service.name`, `k8s.pod.name`) for each simulated instance. Use the same placeholders as other scenarios: `{{.InstanceID}}`, `{{.UUID}}`, `{{.RandomHex n}}`, `{{.ModFrom .InstanceID "a" "b"}}`, etc.
2. **Add an AppProfile (optional)**: For custom log message patterns, add a new profile in `internal/loggen/profiles.go` and register it in `GetAppProfile()`. The profile defines severity weights and message templates with `ArgGenerator` placeholders. If no profile matches the path, the receiver falls back to `GenericProfile(serviceName)`.
3. **Use built-in or external path**: For `builtin/<name>`, place the template in `internal/logstmpl/builtin/`. For external paths, use an absolute or relative path to a directory containing the template file.
4. **Configure in `scenarios`**: Add an entry with `path`, `scale`, and `logs_per_interval`.

Example external template (`/path/to/custom-resource-attributes.yaml`):

```yaml
resourceLogs:
  - resource:
      attributes:
        - key: service.name
          value:
            stringValue: "myapp-{{.InstanceID}}"
        - key: k8s.pod.name
          value:
            stringValue: "pod-{{.InstanceID}}"
    scopeLogs:
      - scope:
          name: "log-generator"
        logRecords: []
```

Example configuration:
```yaml
receivers:
  logsgen:
    start_time: "2025-01-01T00:00:00Z"
    end_time: "2025-01-01T01:00:00Z"
    interval: 10s
    exit_after_end: true
    seed: 123
    scenarios:
      - path: builtin/k8s-nginx
        scale: 30
        logs_per_interval: 50

exporters:
  nop:

service:
  pipelines:
    logs:
      receivers: [logsgen]
      exporters: [nop]
```

### Exporter settings

Multiple exporter settings are already in the config. Adjust the outputs needed. In case the logsgenreceiver is sending data to a stack setup with elastic-package, the following exporter config
for Elasticsearch has to be used (assuming it runs on localhost):

```
exporters:
  elasticsearch:
    endpoint: "https://localhost:9200"
    mapping:
      mode: otel
    num_workers: 10
    user: elastic
    password: changeme
    tls:
      insecure_skip_verify: true
```

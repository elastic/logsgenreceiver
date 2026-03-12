# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

logsgenreceiver is an OpenTelemetry Collector receiver that generates deterministic, synthetic log data for benchmarking log ingest pipelines. It simulates multiple services (nginx, mysql, redis, go microservices, HTTP proxy) running on Kubernetes with realistic severity distributions, field cardinality, and volume patterns.

## Build Commands

```bash
make install-ocb   # Download OTel collector builder (v0.139.0 required)
make build         # Build custom OTel collector → ./logsgen-dev/logsgen
make test          # Run tests with race detector
make bench         # Run benchmarks
make lint          # Run golangci-lint
make vet           # Run go vet
make generate      # Run go generate (mdatagen)
make run           # Build and run with otelcol.dev.yaml
```

Run a single test:
```bash
cd logsgenreceiver && go test -race -run TestName ./...
```

## Architecture

The main Go module is in `logsgenreceiver/`. Key files:

- **receiver.go** - Core receiver implementing OTel's `receiver.Logs` interface. Manages log generation loop, scenario execution, and multi-stage volume multipliers (base rate × diurnal × volume profile × instance skew).
- **config.go** - Configuration structs with comprehensive validation. Main structs: `Config`, `ScenarioCfg`.
- **factory.go** - Standard OTel receiver factory pattern.

### Internal Packages

- **internal/loggen/** - Log generation engine with service-specific profiles (nginx, mysql, redis, goapp, proxy). Each profile defines `AppProfile` with severity weights and `MessageTemplate` entries.
- **internal/logstmpl/** - Template loading and rendering. Loads resource-attributes YAML/JSON templates. Embedded templates in `builtin/` subdirectory.
- **internal/logstats/** - Sharded statistics collection for thread-safe aggregation.

### Key Design Patterns

1. **Deterministic output** - Same config + seed produces identical logs. All RNG flows from single seeded source.
2. **Scenario-based** - Multiple log sources per receiver, each with own scale, profiles, and templates.
3. **Volume shaping pipeline** - `logs_per_interval × diurnal_multiplier(time) × volume_multiplier(rng) × instance_multiplier[i]`

## Adding New Log Types

1. Create `<path>-resource-attributes.yaml` with OTLP format resource attributes. Use placeholders: `{{.InstanceID}}`, `{{.UUID}}`, `{{.RandomHex n}}`, `{{.ModFrom .InstanceID "a" "b"}}`.
2. Optionally add an `AppProfile` in `internal/loggen/profiles.go` and register in `GetAppProfile()`.
3. For builtin profiles, place templates in `internal/logstmpl/builtin/`.
4. Configure in `scenarios` with `path`, `scale`, and `logs_per_interval`.

## Dependencies

- Go 1.24.4
- OTel Collector Builder (ocb) v0.139.0 - **exact version required**
- OTel Collector v1.40.0 (core), v0.134.0 (contrib)

Update OTel version across all files:
```bash
make update-otel-version NEW_VERSION=x.y.z
```

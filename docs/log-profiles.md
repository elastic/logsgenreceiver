# Built-in Profiles

Built-in profiles are named, pre-configured scenario sets for the [logsgenreceiver](https://github.com/elastic/logsgenreceiver) that reproduce realistic workload shapes without manual per-scenario configuration. Reference a profile by name in the receiver config with `profile: "<name>"`.

> [!NOTE]
> All volume figures on this page assume a 10s receiver interval (`interval: 10s`). Daily estimates are base-rate only (`scale × logs_per_interval × 8,640 intervals/day`) with no volume modulation applied. If you configure a different interval, scale the per-interval counts proportionally.

---

## Profiles at a glance

| Profile | Scenarios | Est. daily volume (base, 10s interval) | Recommended use |
|:--------|:----------|:---------------------------------------|:----------------|
| `minimal` | 1 | ~432k logs                             | Quick smoke tests and CI validations |
| `k8s-medium-multiapp` | 5 | ~48.6m logs                            | Representative Kubernetes cluster load testing |

---

## `minimal`

A single simple source for quick tests.

**Scenario:** `builtin/simple` — scale 10, 5 logs/interval (50 logs/10s, ~432k logs/day).

No volume modulation, no needles, no concurrency configuration. Use this profile to verify pipeline connectivity and basic receiver function before running heavier workloads.

```yaml
receivers:
  logsgen:
    profile: minimal
```

---

## `k8s-medium-multiapp`

A representative Kubernetes application stack with multiple services and realistic log volume distribution.

### Cluster topology

30 distinct nodes, 298 pods, 9 logical services (nginx, mysql, redis, 5 goapp sub-services, proxy). Pods land on `node = InstanceID % nodes`. Scenarios with overlapping node ranges simulate service colocation — multiple services sharing the same underlying node.

### Scenario volume table

"base/interval" is `scale × logs_per_interval` with no volume modulation applied.

| Scenario | nodes | scale | pods/node | base/interval |
|:---------|------:|------:|----------:|--------------:|
| nginx    |    30 |   120 |         4 |         2,400 |
| mysql    |    10 |    20 |         2 |           120 |
| redis    |    10 |    50 |        ~5 |           400 |
| goapp    |    27 |    54 |         2 |           540 |
| proxy    |    18 |    54 |         3 |         2,160 |
| **total** |   — |   298 |         — |    **≈5,620** |

### Node colocation tiers

Pods land on `node = InstanceID % nodes`. With nginx covering all 30 nodes and other scenarios covering subsets, the 30 distinct nodes break into four colocation tiers:

| Node range | Count | Services present |
|:-----------|------:|:-----------------|
| 0–9        |    10 | nginx, mysql, redis, goapp, proxy |
| 10–17      |     8 | nginx, goapp, proxy |
| 18–26      |     9 | nginx, goapp |
| 27–29      |     3 | nginx only |

---

### Needle injections

Needles are low-rate fault patterns injected into the log stream at a fixed proportion of emitted records. Use them to validate alerting rules, detection pipelines, or log parsing logic without relying on real incidents.

| Scenario | Needle name | Severity | Rate (per log) | Message excerpt |
|:---------|:------------|:---------|---------------:|:----------------|
| nginx | `upstream_timeout` | ERROR | 0.0004 | `upstream timed out (110: Connection timed out) while connecting to upstream` |
| mysql | `deadlock` | FATAL | 0.0005 | `LATEST DETECTED DEADLOCK in transaction 0x7f2a1c003e80` |
| mysql | `disk_full` | FATAL | 0.0002 | `No space left on device writing to /var/lib/mysql/ibdata1` |
| goapp | `panic_recovery` | FATAL | 0.0001 | `{"level":"fatal","msg":"panic recovered","error":"runtime error: index out of range [5] with length 3"}` |
| proxy | `upstream_unavailable` | ERROR | 0.001 | `no healthy upstream instances available` |

---

### Volume modulation

Four of the five scenarios configure a `volume_profile` to introduce realistic burst and quiet periods. Redis does not configure a `volume_profile`.

| Parameter | Typical value | Effect |
|:----------|:-------------|:-------|
| `burst_probability` | 0.3 | Probability per interval of entering a burst |
| `burst_multiplier_min` | 2.0 | Minimum burst multiplier applied to `logs_per_interval` |
| `burst_multiplier_max` | 10.0 | Maximum burst multiplier applied to `logs_per_interval` |
| `burst_duration_min` | 8 intervals | Minimum burst duration |
| `burst_duration_max` | 24–30 intervals | Maximum burst duration |
| `quiet_probability` | 0.03 | Probability per interval of entering a quiet period |
| `quiet_multiplier` | 0.2 | Quiet-period rate multiplier |
| `quiet_duration_min/max` | 8–24 intervals | Duration of quiet period |

---

### Trace context and concurrency

| Scenario | `emit_trace_context` | `concurrency` | Notes |
|:---------|:---------------------|:-------------|:------|
| nginx | not set | 4 | No trace IDs emitted |
| mysql | not set | 1 | Single-threaded |
| redis | not set | 2 | — |
| goapp | `false` | 6 | Explicitly disabled; use when testing pipelines that must not receive trace context |
| proxy | `true` | 6 | Emits `trace_id` and `span_id`; use when testing distributed trace correlation |

When `emit_trace_context: true`, the receiver adds `trace_id`, `span_id`, and `trace_flags` attributes to each log record. This is useful for validating APM correlation rules, Elastic's `logs-to-traces` feature, or OpenTelemetry TraceContext propagation.

---

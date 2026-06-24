# Composite DRA Driver

A generic, config-driven Kubernetes [Dynamic Resource Allocation (DRA)](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/) driver that composes devices from multiple underlying DRA drivers into single allocatable units.

For example, pairing GPUs from `gpu.nvidia.com` with RDMA NICs from `dra.net` by PCIe root topology — presenting each GPU-NIC pair as one composite device the scheduler allocates natively.

## Why

The previous approach ([dra-rail-admission-webhook](https://github.com/openshift-psap/dra-rail-admission-webhook)) scanned ResourceSlices, picked nodes, selected specific devices, and pinned pods via nodeAffinity — bypassing the Kubernetes scheduler entirely. This driver moves all scheduling decisions back to the scheduler while preserving topology-aware device pairing.

## How It Works

```
Underlying Drivers                    Composite Driver                      User
┌──────────────┐                     ┌─────────────────────┐
│gpu.nvidia.com│─ ResourceSlices ──▶│ Synthesizer         │
│  (8 GPUs)    │                    │  watch → pair →     │
└──────────────┘                    │  publish composite  │──▶ ResourceSlices
┌──────────────┐                    │  ResourceSlices     │    (8 GPU-NIC pairs)
│   dra.net    │─ ResourceSlices ──▶│                     │
│  (8 NICs)    │                    ├─────────────────────┤         │
└──────────────┘                    │ Plugin (DRAPlugin)  │         ▼
                                    │  PrepareResources:  │    ┌──────────┐
                  ┌─── gRPC ◀───────│  1. shadow claims   │    │Scheduler │
                  │                 │  2. gRPC delegate   │◀───│allocates │
                  ▼                 │  3. return CDI IDs  │    │natively  │
           Underlying drivers       └─────────────────────┘    └──────────┘
           prepare their own
           devices as usual
```

**Shadow Claims Pattern**: On `PrepareResourceClaims`, the composite driver creates real ResourceClaims ("shadow claims") for each underlying driver with pre-filled allocation results, then calls their gRPC sockets to prepare hardware. Neither nvidia nor dranet checks if claims are in `pod.spec.resourceClaims` — validated on real hardware.

## Quick Start

### Install

```bash
# Driver only (webhook auto-detected based on K8s version)
helm install composite charts/composite-dra-driver \
  -n composite-dra-system \
  -f charts/composite-dra-driver/values-poseidon.yaml
```

### Request GPU-NIC Pairs

**K8s 1.36+ (DRAExtendedResource beta, no webhook needed):**
```yaml
containers:
- resources:
    requests:
      composite.dra.example.io/gpu-nic-pair: "4"
```

**K8s < 1.36 (webhook intercepts synthetic resource):**
```yaml
containers:
- resources:
    requests:
      composite.dra/gpu-nic-pair: "4"
    limits:
      composite.dra/gpu-nic-pair: "4"  # must match requests
```

**Manual ResourceClaimTemplate (any K8s version, no webhook):**
```yaml
apiVersion: resource.k8s.io/v1
kind: ResourceClaimTemplate
metadata:
  name: my-pairs
spec:
  spec:
    devices:
      requests:
      - name: pair-0
        exactly:
          deviceClassName: composite-gpu-nic
          allocationMode: ExactCount
          count: 1
      - name: pair-1
        exactly:
          deviceClassName: composite-gpu-nic
          allocationMode: ExactCount
          count: 1
```

### Verify

```bash
# Composite devices published
kubectl get resourceslices -o custom-columns='DRIVER:.spec.driver' | sort | uniq -c

# Inside pod
kubectl exec <pod> -- nvidia-smi -L          # GPUs
kubectl exec <pod> -- ip -br addr            # NICs (net0, net1, ...)
kubectl exec <pod> -- rdma link show         # RDMA devices
```

## Features

- **Generic composition** — config-driven, not hardcoded to GPU+NIC. Add any DRA driver via config.
- **Scheduler-native** — no node pinning, no allocation bypass. Scheduler decides everything.
- **Shadow claims** — delegates hardware prep to underlying drivers via gRPC. Zero driver-specific code.
- **CEL filters** — filter source devices (e.g., `rdma == true`) before pairing.
- **Per-rail NIC config** — routing tables, gateways, cross-rail routes embedded in shadow claims.
- **Auto-detect webhook** — Helm chart auto-deploys webhook on K8s < 1.36, skips on 1.36+ (DRAExtendedResource).
- **Parallel prepare** — shadow claims created concurrently. 8 pairs in ~3s (bottleneck: nvidia CDI gen).
- **Crash recovery** — BoltDB persistence + orphan reconciler.
- **HA** — driver: DaemonSet (one per node). Webhook: multi-replica Deployment.

## AI-Assisted Development

Built with [Claude Code](https://claude.ai/code) (Claude Opus 4.6).

## Configuration

```yaml
driver:
  name: "composite.dra.example.io"

sources:
  - name: gpu
    driver: "gpu.nvidia.com"
    deviceClassName: "gpu.nvidia.com"
    forwardAttributes:
      - domain: "resource.kubernetes.io"
        attributes: ["pciBusID", "pcieRoot"]
  - name: nic
    driver: "dra.net"
    deviceClassName: "dranet"
    forwardAttributes:
      - domain: "dra.net"
        attributes: ["pciAddress", "numaNode", "rdma", "ipv4"]

compositions:
  - name: "gpu-nic-pair"
    members:
      - source: gpu
        count: 1
      - source: nic
        count: 1
    constraints:
      - type: matchAttribute
        attribute: "resource.kubernetes.io/pcieRoot"
    filters:
      nic:
        cel: 'device.attributes["dra.net"].rdma == true'

deviceParams:
  configMapPath: "/etc/composite-dra/device-params/params.yaml"
```

Opaque driver params (routes, gateways, MTU, etc.) are provided via an external ConfigMap — the composite driver never interprets their content. See [examples/](charts/composite-dra-driver/examples/) for ConfigMap templates and [values.yaml](charts/composite-dra-driver/values.yaml) for all options.

## Observability

The driver and webhook expose Prometheus metrics, emit Kubernetes Events, and use structured logging.

### Prometheus Metrics

Both binaries serve `/metrics` on port 8080 (configurable via `--metrics-port`).

**Driver metrics** (labeled by `composition`):

| Metric | Type | Description |
|--------|------|-------------|
| `composite_dra_synthesis_devices_total` | Gauge | Composite devices published per composition |
| `composite_dra_synthesis_duration_seconds` | Histogram | Synthesis pipeline duration |
| `composite_dra_claims_active` | Gauge | Composite claims currently prepared |
| `composite_dra_shadow_claims_active` | Gauge | Shadow claims currently active (N per composite claim) |
| `composite_dra_prepare_duration_seconds` | Histogram | End-to-end Prepare time |
| `composite_dra_prepare_shadow_create_duration_seconds` | Histogram | Shadow claim creation phase |
| `composite_dra_prepare_grpc_duration_seconds` | Histogram | gRPC delegation phase |
| `composite_dra_grpc_errors_total` | Counter | gRPC errors (also labeled by `source_driver`) |
| `composite_dra_device_params_errors_total` | Counter | Device parameter resolution failures |
| `composite_dra_reconciler_claims_cleaned_total` | Counter | Orphaned shadow claims GC'd |

**Webhook metrics:**

| Metric | Type | Description |
|--------|------|-------------|
| `composite_dra_webhook_mutations_total` | Counter | Pods mutated (labeled by `composition`) |
| `composite_dra_webhook_skipped_total` | Counter | Pods skipped (labeled by `reason`) |
| `composite_dra_webhook_errors_total` | Counter | Errors by stage (unmarshal, mutate, marshal) |
| `composite_dra_webhook_duration_seconds` | Histogram | Mutation request latency |
| `composite_dra_webhook_templates_created_total` | Counter | ResourceClaimTemplates created |
| `composite_dra_webhook_reconciler_templates_cleaned_total` | Counter | Stale templates GC'd |

### Scraping Setup

**OpenShift** (built-in monitoring):
1. Enable user workload monitoring (`enableUserWorkload: true` in `cluster-monitoring-config` ConfigMap)
2. Set `metrics.serviceMonitor.enabled=true` in Helm values
3. Metrics appear in **Observe > Metrics** in the OpenShift console

**Vanilla Kubernetes:**
1. Install [kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack) (provides Prometheus Operator + ServiceMonitor CRD)
2. Set `metrics.serviceMonitor.enabled=true` in Helm values
3. Prometheus Operator auto-discovers the ServiceMonitor

**Without Prometheus Operator:** scrape `<pod-ip>:8080/metrics` directly via static Prometheus config or any OpenMetrics-compatible collector.

### Kubernetes Events

Events are emitted on composite ResourceClaims during prepare/unprepare:

| Event | When |
|-------|------|
| `PrepareStarted` | Shadow claim creation beginning |
| `PrepareCompleted` | All shadow claims + gRPC done (includes timing) |
| `PrepareFailed` | Which driver/device failed |
| `UnprepareCompleted` | Cleanup done |

View with `kubectl describe resourceclaim <name>` or `kubectl get events --field-selector reason=PrepareCompleted`.

### Structured Logging

All log output uses `klog.InfoS`/`ErrorS` with key-value pairs. Enable JSON output for log aggregation:

```bash
# In Helm values or container args:
--logging-format=json
```

### Limitations

- Gauge metrics (`claims_active`, `shadow_claims_active`, `synthesis_devices_total`) reset to zero on pod restart. Prometheus handles this via `resets()` for counters; for gauges, expect a brief dip after restarts.
- Metrics port (8080) is plaintext HTTP. If your security policy requires TLS on all ports, a sidecar proxy (e.g., kube-rbac-proxy) is needed.
- `synthesis_duration_seconds` records the full pairer+publisher pipeline time. When multiple compositions exist, each gets the same duration value (single `recompute()` call processes all compositions).

## Requirements

- Kubernetes 1.34+ with DRA enabled
- Go 1.26 (build)
- Underlying DRA drivers deployed (e.g., nvidia GPU driver, dranet)
- OpenShift: SCC for hostPath volumes (auto-created by Helm chart)

## Known Limitations

- **No NUMA affinity enforcement** — composite devices carry `numaNode` as an attribute but the webhook does not generate MatchAttribute constraints. NUMA packing requires manual ResourceClaimTemplates with explicit constraints. ([#1](https://github.com/openshift-psap/composite-dra-driver/issues/1), [Discussion #11](https://github.com/openshift-psap/composite-dra-driver/discussions/11))

- **Device sharing conflict across compositions** — when multiple compositions share a source (e.g. GPU appears in both `gpu` and `gpu-nic-pair`), the scheduler can allocate the same physical device to both compositions on the same node. Each composition publishes an independent pool — the scheduler has no cross-pool mutual exclusion. Safe when pods land on different nodes or only one composition is actively used at a time. Fix requires pairer-side device partitioning. ([#28](https://github.com/openshift-psap/composite-dra-driver/issues/28))

- **VF support requires external IPAM** — PF mode works with the external device params ConfigMap. VF mode needs an external controller to allocate IPs and populate the ConfigMap (VFs lack `dra.net/ipv4`). ([#34](https://github.com/openshift-psap/composite-dra-driver/issues/34))

- **Webhook required on K8s < 1.36** — the `DRAExtendedResource` feature gate is beta (on by default) in K8s 1.36, eliminating the need for the webhook. On K8s 1.35, the gate exists as alpha and can be manually enabled — see [Method 3 in the cheatsheet](docs/CHEATSHEET.md#method-3-extended-resource-k8s-135-with-draextendedresource-gate) and the [FAQ entry on dropping the webhook](docs/FAQ.md#when-can-we-drop-the-webhook-entirely). On K8s 1.34, the webhook is the only option for the resource request UX.

- **Extended resources require limits == requests** — Kubernetes requires this for all extended resources. Affects StatefulSet/LWS pod templates. Not a bug — K8s API constraint.

## Documentation

- [Cheatsheet](docs/CHEATSHEET.md) — install, request methods, verify, troubleshoot
- [FAQ](docs/FAQ.md) — architecture decisions, scheduling, networking, performance
- [HA Design](docs/HA-DESIGN.md) — failure scenarios, DaemonSet vs Deployment HA
- [Status](docs/STATUS.md) — implementation status and validation evidence
- [Agents](docs/AGENTS.md) — instructions for AI coding agents

## License

Apache 2.0

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

railConfig:
  crossRailCIDR: "10.0.0.0/13"
  rails:
    - selector:
        cel: 'device.attributes["dra.net"].ipv4.startsWith("10.0.")'
      config:
        subnet: "10.0.0.0/16"
        gateway: "10.0.0.1"
        mtu: 9000
        tableID: 100
```

See [values.yaml](charts/composite-dra-driver/values.yaml) for all options.

## Requirements

- Kubernetes 1.34+ with DRA enabled
- Go 1.26 (build)
- Underlying DRA drivers deployed (e.g., nvidia GPU driver, dranet)
- OpenShift: SCC for hostPath volumes (auto-created by Helm chart)

## Documentation

- [CHEATSHEET.md](CHEATSHEET.md) — install, request methods, verify, troubleshoot
- [FAQ.md](FAQ.md) — architecture decisions, scheduling, networking, performance
- [HA-DESIGN.md](HA-DESIGN.md) — failure scenarios, DaemonSet vs Deployment HA
- [STATUS.md](STATUS.md) — implementation status and validation evidence
- [AGENTS.md](AGENTS.md) — instructions for AI coding agents

## License

Apache 2.0

# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Test Commands

```bash
make build              # Build both driver and webhook binaries
make build-driver       # Build composite-dra-driver only
make build-webhook      # Build composite-dra-webhook only
make test               # Run all tests (go test ./... -v)
make lint               # Run go vet ./...
make mod-tidy           # go mod tidy
make image              # Build both container images (podman)
make deploy             # kubectl apply all manifests in deploy/

# Single package test
go test ./pkg/synthesizer/... -v -run TestPairDevices

# CI runs with race detector
go test ./... -v -race
```

Go version: 1.26. Module: `github.com/openshift-psap/composite-dra-driver`.

## Architecture

This is a Kubernetes **Dynamic Resource Allocation (DRA)** driver that composes devices from multiple underlying DRA drivers (e.g. GPU + NIC) into single allocatable units. Two binaries:

**Driver** (`cmd/driver/main.go`) — DaemonSet on every node. Registers as a kubelet DRA plugin via gRPC socket. Pipeline:

1. **Synthesizer** watches underlying drivers' ResourceSlices via informers
2. **Pairer** groups devices across sources using `matchAttribute` constraints (e.g. same PCIe root) and CEL filters
3. **Publisher** builds composite ResourceSlices (splits at 128-device K8s limit) and publishes via kubelet helper
4. **DeviceStore** maps composite device names → underlying device members (thread-safe, in-memory)
5. On allocation, **CompositePlugin.Prepare** creates **shadow ResourceClaims** for each underlying driver with pre-filled allocation, then calls each driver's gRPC `NodePrepareResources`
6. **StateStore** (BoltDB) persists shadow claim records for crash recovery
7. **Reconciler** (5-min loop) garbage-collects orphaned shadow claims

**Webhook** (`cmd/webhook/main.go`) — Mutating admission webhook. Intercepts synthetic resource request `composite.dra/gpu-nic-pair: N` from pod containers, strips it from requests/limits, generates a ResourceClaimTemplate with N device pair requests, and patches the pod spec with claim refs.

## Key Packages

| Package | Role |
|---------|------|
| `pkg/plugin` | DRA plugin (Prepare/Unprepare), gRPC client to underlying drivers, orphan reconciler |
| `pkg/synthesizer` | Watcher → Pairer → Publisher pipeline, CEL filter evaluation |
| `pkg/shadow` | Shadow claim CRUD (`ClaimManager`), rail config resolution for NIC opaque params |
| `pkg/store` | `DeviceStore` (in-memory device mappings), `StateStore` (BoltDB persistence) |
| `pkg/config` | Config types, YAML loading, validation |
| `pkg/webhook` | HTTP handler, pod mutator, claim builder |

## Shadow Claims Pattern

Core design: the composite driver doesn't implement device logic. Instead:
- Prepare creates real K8s ResourceClaims ("shadows") with pre-filled allocation pointing to underlying drivers
- Calls each underlying driver via gRPC at `/var/lib/kubelet/plugins/<driver>/dra.sock`
- Combines CDI device IDs from all underlying drivers
- Shadow claims have OwnerReference → composite claim for GC cascade
- Unprepare reverses: gRPC unprepare calls, then deletes shadow claims

## Configuration

Config YAML (`/etc/composite-dra/config.yaml` in-cluster) defines:
- `driver.name` — composite driver name (e.g. `composite.dra.llm-d.io`)
- `sources[]` — underlying drivers with forwarded attributes
- `compositions[]` — pairing rules: which sources, member counts, matchAttribute constraints, CEL filters
- `railConfig` — per-rail NIC config (subnet, gateway, MTU, routing table) embedded in shadow claims as opaque params

## Deployment

- DaemonSet runs as `privileged` on all nodes (including control-plane), priority `system-node-critical`
- Mounts kubelet plugin dirs and BoltDB state dir as hostPath volumes
- Config delivered via ConfigMap
- DeviceClass manifest tells scheduler about the composite driver
- Helm chart in `charts/composite-dra-driver/` with values files for different clusters
- OpenShift: SCC manifest in `deploy/scc.yaml`

## Testing

Table-driven unit tests. Helpers in pairer_test.go (`strAttr`, `intAttr`, `boolAttr`) for building device attributes. No integration tests — those require a K8s cluster with DRA feature gate. Test files: config validation, pairer algorithm, device store thread-safety, publisher splitting, rail config, webhook claim builder.

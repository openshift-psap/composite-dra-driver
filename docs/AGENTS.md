# AGENTS.md

Instructions for AI coding agents working on this repository.

## Quick Start

```bash
# Go 1.26 required — if not default:
~/go/bin/go1.26.3 build ./...
~/go/bin/go1.26.3 test ./...

# Or if Go 1.26 is default:
make build && make test
```

## Repository Map

```
cmd/driver/main.go         ← DaemonSet binary entry point
cmd/webhook/main.go         ← Webhook binary entry point
pkg/plugin/plugin.go        ← DRAPlugin (Prepare/Unprepare) — shadow claims + gRPC
pkg/plugin/grpc_client.go   ← gRPC client to underlying driver sockets
pkg/plugin/reconciler.go    ← Orphan shadow claim cleanup
pkg/synthesizer/            ← Watch → Pair → Publish pipeline
pkg/shadow/claims.go        ← Shadow ResourceClaim CRUD
pkg/shadow/params.go        ← External device params resolver (generic, driver-agnostic)
pkg/store/                  ← DeviceStore (in-memory) + StateStore (BoltDB)
pkg/config/                 ← Config types + validation
pkg/webhook/                ← Mutating webhook (resource request → claim template)
charts/composite-dra-driver/ ← Helm chart (driver + optional webhook)
```

## Key Design Decisions — Don't Undo These

1. **Shadow claims pattern** — the driver creates real ResourceClaims for underlying drivers, not synthetic ones. Both nvidia and dranet were validated to accept shadow claims (they don't check pod.spec.resourceClaims). Don't try gRPC passthrough without real claims — the kubeletplugin.Helper fetches claims from the API server.

2. **No NUMA constraints by default** — removed intentionally (Discussion #11). MatchAttribute on numaNode is a hard constraint that causes Pending pods when a single NUMA zone lacks capacity. Don't add NUMA constraints back without an opt-in mechanism.

3. **Resource requests, not annotations** — webhook intercepts `resources.requests["composite.dra/gpu-nic-pair"]`, strips it, replaces with claim template. Users must set both `requests` and `limits` (K8s extended resource rule). This matters for StatefulSet/LWS controllers.

4. **client-go QPS=100, Burst=200** — default QPS=5 serializes parallel shadow claim creation. Don't lower this.

5. **Cross-rail routes in main table** — not in per-rail policy tables. Policy rules divert same-rail traffic; cross-rail needs main table for gateway routing. See original webhook templates at `openshift-psap/ibmcloud-roce-dra-net-setup/h100/`.

6. **Generic config-driven** — don't hardcode driver names, device classes, or attribute names. Everything comes from config. Adding a new underlying driver should be config-only.

## What's Coupled (Known Technical Debt)

- External device params ConfigMap format is opaque JSON templates — no schema validation at load time. Errors surface at Prepare time when template execution fails.
- VF mode requires external IPAM to populate the ConfigMap (issue #34).

## Testing Conventions

- Table-driven tests with helpers: `strAttr()`, `intAttr()`, `boolAttr()` for DeviceAttribute construction
- Test files colocated: `pairer_test.go` next to `pairer.go`
- CI runs `go test ./... -v -race`
- No integration tests in-tree — cluster tests done manually on poseidon

## Cluster Testing (Poseidon)

```bash
export KUBECONFIG=<path-to-cluster-kubeconfig>

# Deploy via Helm
helm install composite charts/composite-dra-driver \
  -n composite-dra-system \
  -f charts/composite-dra-driver/values-poseidon.yaml \
  --set webhook.enabled=true \
  --set webhook.tls.certManager.issuerRef.name=composite-dra-selfsigned

# Label namespace for webhook
oc label ns <ns> composite.dra/webhook-enabled=true

# Test pod
oc apply -f - <<EOF
apiVersion: v1
kind: Pod
metadata:
  name: test
  namespace: <ns>
spec:
  containers:
  - name: test
    image: quay.io/dagray/rdma-tools:tiny
    command: ["sleep", "300"]
    resources:
      requests:
        composite.dra/gpu-nic-pair: "2"
      limits:
        composite.dra/gpu-nic-pair: "2"
EOF

# Verify
oc get resourceclaims -n <ns>                     # shadow claims visible
oc exec test -- ip -br addr                        # net0, net1 with IPs
oc exec test -- rdma link show                     # mlx5 devices
oc logs -l app.kubernetes.io/component=driver \
  -n composite-dra-system | grep "plugin:"         # prepare timing
```

## CI / Image Builds

- Push to main → builds + pushes `latest` images to ghcr.io
- PR → runs `go vet` + `go test -race` + `go build` (no push)
- Comment `/build-approved` on PR → builds `pr-<number>` tagged images (maintainer gate)
- Images: `ghcr.io/openshift-psap/composite-dra-{driver,webhook}:<tag>`
- Private repo — cluster needs `ghcr-pull` secret with classic PAT (`read:packages` + `repo` scopes)

## Open Issues to Be Aware Of

| Issue | Impact | Status |
|-------|--------|--------|
| #4 Attribute deduplication | 3 copies of pcieRoot per device | Design pending |
| #5 Prepare performance | gRPC phase ~3s (nvidia CDI gen) | Phase 1 done, Phase 2 pending |
| #7 Combined shadow claims | Could halve API calls | Design pending |
| #8 CI improvements | No golangci-lint yet | Partial |
| #9 Decouple dranet config | railconfig.go was dranet-specific | **Resolved** (PR #32 — external device params) |
| #13 Auto-detect webhook | Manual enable/disable | Design pending |

## Reference Repos

- [kubernetes/kubernetes](https://github.com/kubernetes/kubernetes) — K8s source (kubeletplugin at `staging/src/k8s.io/dynamic-resource-allocation/`, DRA APIs at `staging/src/k8s.io/api/resource/v1/`)
- [kubernetes-sigs/dranet](https://github.com/kubernetes-sigs/dranet) — dranet DRA driver (NRI hooks, PodConfigStore)
- [openshift-psap/dra-rail-admission-webhook](https://github.com/openshift-psap/dra-rail-admission-webhook) — old webhook (reference for VF/IPAM implementation)
- [kubernetes-sigs/dra-driver-nvidia-gpu](https://github.com/kubernetes-sigs/dra-driver-nvidia-gpu) — nvidia DRA driver (CDI, checkpoint)

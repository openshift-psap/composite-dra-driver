# Composite DRA Driver — Cheat Sheet

## Install

```bash
# Driver only (webhook auto-detected by K8s version)
helm install composite charts/composite-dra-driver \
  -n composite-dra-system \
  --create-namespace \
  -f charts/composite-dra-driver/values-poseidon.yaml

# With Prometheus ServiceMonitor (requires Prometheus Operator or OpenShift user workload monitoring)
helm install composite charts/composite-dra-driver \
  -n composite-dra-system \
  --create-namespace \
  -f charts/composite-dra-driver/values-poseidon.yaml \
  --set metrics.serviceMonitor.enabled=true

# Driver + webhook explicitly enabled
helm install composite charts/composite-dra-driver \
  -n composite-dra-system \
  --create-namespace \
  -f charts/composite-dra-driver/values-poseidon.yaml \
  --set webhook.mode=enabled

# Upgrade (e.g., enable metrics scraping later)
helm upgrade composite charts/composite-dra-driver \
  -n composite-dra-system \
  -f charts/composite-dra-driver/values-poseidon.yaml \
  --set metrics.serviceMonitor.enabled=true
```

**Prerequisites for metrics scraping:**
- **OpenShift:** user workload monitoring must be enabled (`enableUserWorkload: true` in `cluster-monitoring-config` ConfigMap in `openshift-monitoring` namespace)
- **Vanilla K8s:** [kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack) or equivalent providing the ServiceMonitor CRD
- **RBAC:** Events permissions (create/patch) are included in the Helm chart ClusterRole automatically — no additional setup needed

## Request GPU-NIC Pairs

### Method 1: Webhook resource request (simplest)

Requires: webhook enabled, namespace labeled.

```bash
# Label namespace for webhook interception
oc label ns <namespace> composite.dra/webhook-enabled=true
```

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
spec:
  containers:
  - name: app
    image: my-image
    resources:
      requests:
        composite.dra/gpu-nic-pair: "4"
      limits:
        composite.dra/gpu-nic-pair: "4"    # must match requests (K8s extended resource rule)
```

Webhook strips the synthetic resource from both requests and limits, creates a ResourceClaimTemplate, and patches the pod with claim refs.

**Note:** Both `requests` and `limits` must be set and equal — K8s requires this for extended resources. This is especially important when using StatefulSet, LeaderWorkerSet, or other controllers that validate the pod template before pod creation.

### Method 2: Manual ResourceClaimTemplate

No webhook required. Works on any namespace.

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
      # ... add more pair-N for more pairs
---
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
spec:
  containers:
  - name: app
    image: my-image
    resources:
      claims:
      - name: pairs
        request: pair-0
      - name: pairs
        request: pair-1
  resourceClaims:
  - name: pairs
    resourceClaimTemplateName: my-pairs
```

### Method 3: Extended resource (K8s 1.35+ with DRAExtendedResource gate)

No webhook, no claim templates. Requires DeviceClass with `extendedResourceName`.

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
spec:
  containers:
  - name: app
    image: my-image
    resources:
      requests:
        composite.dra.example.io/gpu-nic-pair: "4"
```

## Verify

```bash
# Check composite devices published
oc get resourceslices -o custom-columns='DRIVER:.spec.driver' --no-headers | sort | uniq -c

# Check inside pod
oc exec <pod> -- nvidia-smi -L                    # GPUs visible
oc exec <pod> -- ip -br addr                       # NICs (net0, net1, ...)
oc exec <pod> -- ip route show table <tableID>     # per-rail routing
oc exec <pod> -- ip rule show                      # policy rules
oc exec <pod> -- rdma link show                    # RDMA devices

# Check shadow claims
oc get resourceclaims -n <namespace>

# Check driver logs
oc logs -l app.kubernetes.io/component=driver -n composite-dra-system --tail=20
```

## Configuration

### Cluster-specific values file

Override `deviceParams`, `deviceParamsConfigMap`, and `openshift.scc.enabled` per cluster. See `values-poseidon.yaml` and `values-b200-pf.yaml` for examples.

Key settings:

| Value | Description |
|-------|-------------|
| `driver.name` | Driver name in ResourceSlices (default: `composite.dra.io`) |
| `sources` | List of underlying DRA drivers to compose |
| `compositions` | Pairing rules (matchAttribute constraints) |
| `compositions[].filters` | CEL filters per source (e.g., `rdma == true`) |
| `deviceParamsConfigMap` | External ConfigMap name with opaque driver params |
| `deviceParams.configMapPath` | Mount path for the device params file |
| `webhook.mode` | `auto` / `enabled` / `disabled` |
| `webhook.tls.mode` | `cert-manager` (default) / `helm-generated` / `manual` |
| `openshift.scc.enabled` | Create SCC for hostPath (OCP only) |

## Known Limitations

**Device sharing conflict across compositions (#28):** When multiple compositions share a source (e.g. GPU appears in both `gpu` and `gpu-nic-pair`), the scheduler can allocate the same physical device to both compositions on the same node. The underlying driver rejects the second allocation. This happens because each composition publishes an independent pool — the scheduler has no cross-pool mutual exclusion. Safe to use when pods land on different nodes or only one composition is actively used at a time. Fix requires pairer-side device partitioning.

## Observability

```bash
# Check metrics (port-forward to any driver/webhook pod)
oc port-forward -n composite-dra-system <driver-pod> 8080:8080
curl -s localhost:8080/metrics | grep composite_dra

# Key metrics
curl -s localhost:8080/metrics | grep composite_dra_synthesis_devices_total    # devices published
curl -s localhost:8080/metrics | grep composite_dra_claims_active              # claims prepared
curl -s localhost:8080/metrics | grep composite_dra_shadow_claims_active       # shadow claims

# K8s Events on ResourceClaims
oc describe resourceclaim <name> -n <namespace>    # shows PrepareStarted/Completed/Failed
oc get events --field-selector reason=PrepareCompleted

# OpenShift: metrics in console
# Observe > Metrics > composite_dra_synthesis_devices_total

# Enable ServiceMonitor scraping
helm upgrade composite ... --set metrics.serviceMonitor.enabled=true
```

## Troubleshooting

```bash
# Driver not publishing slices
oc logs <driver-pod> -n composite-dra-system | grep -E "synth|pairer|publisher"

# Shadow claim creation failing
oc logs <driver-pod> -n composite-dra-system | grep -E "shadow|grpc|plugin"

# Webhook not mutating pods
oc logs -l app.kubernetes.io/component=webhook -n composite-dra-system
oc get mutatingwebhookconfigurations composite-dra-driver-webhook -o yaml

# Check namespace label
oc get ns <namespace> --show-labels | grep webhook-enabled
```

## Uninstall

```bash
helm uninstall composite -n composite-dra-system
oc delete ns composite-dra-system
```

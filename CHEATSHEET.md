# Composite DRA Driver — Cheat Sheet

## Install

```bash
# Driver only (no webhook)
helm install composite charts/composite-dra-driver \
  -n composite-dra-system \
  -f charts/composite-dra-driver/values-poseidon.yaml

# Driver + webhook
helm install composite charts/composite-dra-driver \
  -n composite-dra-system \
  -f charts/composite-dra-driver/values-poseidon.yaml \
  --set webhook.enabled=true \
  --set webhook.tls.certManager.issuerRef.name=<issuer-name>

# Upgrade (e.g., enable webhook later)
helm upgrade composite charts/composite-dra-driver \
  -n composite-dra-system \
  -f charts/composite-dra-driver/values-poseidon.yaml \
  --set webhook.enabled=true
```

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

Override `railConfig` and `openshift.scc.enabled` per cluster. See `values-poseidon.yaml` for example.

Key settings:

| Value | Description |
|-------|-------------|
| `driver.name` | Driver name in ResourceSlices (default: `composite.dra.example.io`) |
| `sources` | List of underlying DRA drivers to compose |
| `compositions` | Pairing rules (matchAttribute constraints) |
| `compositions[].filters` | CEL filters per source (e.g., `rdma == true`) |
| `railConfig.rails` | Per-rail NIC routing config |
| `railConfig.crossRailCIDR` | Supernet for cross-rail routes (e.g., `10.0.0.0/13`) |
| `webhook.enabled` | Deploy webhook (default: false) |
| `openshift.scc.enabled` | Create SCC for hostPath (OCP only) |
| `deviceClass.extendedResourceName` | Enable extended resource UX (K8s 1.35+) |

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

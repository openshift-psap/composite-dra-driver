# Composite DRA Driver — Observability

The driver and webhook expose Prometheus metrics, emit Kubernetes Events on ResourceClaims, and use structured logging with key-value pairs.

## Prometheus Metrics

Both binaries serve `/metrics` on port 8080 (configurable via `--metrics-port` flag or `metrics.port` Helm value).

### Driver Metrics

All labeled by `composition` unless noted otherwise.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `composite_dra_synthesis_devices_total` | Gauge | `composition` | Composite devices currently published |
| `composite_dra_synthesis_duration_seconds` | Histogram | `composition` | Synthesis pipeline duration |
| `composite_dra_claims_active` | Gauge | `composition` | Composite claims currently prepared |
| `composite_dra_shadow_claims_active` | Gauge | `composition` | Shadow claims active (N per composite claim, one per device member) |
| `composite_dra_prepare_duration_seconds` | Histogram | `composition` | End-to-end Prepare time |
| `composite_dra_prepare_shadow_create_duration_seconds` | Histogram | `composition` | Shadow claim creation phase (Phase 1) |
| `composite_dra_prepare_grpc_duration_seconds` | Histogram | `composition` | gRPC delegation phase (Phase 2) |
| `composite_dra_grpc_errors_total` | Counter | `composition`, `source_driver` | gRPC errors by composition and underlying driver |
| `composite_dra_device_params_errors_total` | Counter | `composition` | Device parameter resolution failures |
| `composite_dra_reconciler_claims_cleaned_total` | Counter | — | Orphaned shadow claims deleted by reconciler |

### Webhook Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `composite_dra_webhook_mutations_total` | Counter | `composition` | Pods mutated |
| `composite_dra_webhook_skipped_total` | Counter | `reason` | Pods skipped (`no_resource_request`, `already_mutated`) |
| `composite_dra_webhook_errors_total` | Counter | `stage` | Errors by stage (`read`, `decode`, `unmarshal`, `mutate`, `marshal`) |
| `composite_dra_webhook_duration_seconds` | Histogram | — | Mutation request latency |
| `composite_dra_webhook_templates_created_total` | Counter | `composition` | ResourceClaimTemplates created |
| `composite_dra_webhook_reconciler_templates_cleaned_total` | Counter | — | Stale templates deleted by reconciler |

### Useful PromQL Queries

```promql
# Devices available per composition (cluster-wide)
sum by (composition) (composite_dra_synthesis_devices_total)

# Device utilization ratio
sum by (composition) (composite_dra_claims_active)
  / sum by (composition) (composite_dra_synthesis_devices_total > 0)

# Average shadow claims per composite claim
sum(composite_dra_shadow_claims_active) / sum(composite_dra_claims_active)

# Prepare latency p99
histogram_quantile(0.99, sum by (le, composition) (rate(composite_dra_prepare_duration_seconds_bucket[5m])))

# gRPC error rate by driver
rate(composite_dra_grpc_errors_total[5m])

# Webhook mutation rate
rate(composite_dra_webhook_mutations_total[5m])
```

## Scraping Setup

### OpenShift (built-in monitoring)

1. **Enable user workload monitoring** (one-time, cluster-level):
   ```bash
   oc apply -f - <<EOF
   apiVersion: v1
   kind: ConfigMap
   metadata:
     name: cluster-monitoring-config
     namespace: openshift-monitoring
   data:
     config.yaml: |
       enableUserWorkload: true
   EOF
   ```

2. **Enable ServiceMonitor** in Helm values:
   ```bash
   helm install composite charts/composite-dra-driver \
     -n composite-dra-system --create-namespace \
     -f charts/composite-dra-driver/values-poseidon.yaml \
     --set metrics.serviceMonitor.enabled=true
   ```

3. **View in OpenShift console**: Observe > Metrics > `composite_dra_synthesis_devices_total`

### Vanilla Kubernetes

1. **Install Prometheus Operator** (if not already present):
   ```bash
   helm install kube-prometheus-stack prometheus-community/kube-prometheus-stack \
     -n monitoring --create-namespace
   ```

2. **Enable ServiceMonitor** in Helm values:
   ```bash
   helm install composite charts/composite-dra-driver \
     -n composite-dra-system --create-namespace \
     --set metrics.serviceMonitor.enabled=true
   ```

3. Prometheus Operator auto-discovers ServiceMonitors in all namespaces by default.

### Without Prometheus Operator

Scrape `<pod-ip>:8080/metrics` directly via static Prometheus config, Datadog agent, or any OpenMetrics-compatible collector.

```bash
# Quick check via port-forward
kubectl port-forward -n composite-dra-system <driver-pod> 8080:8080
curl -s localhost:8080/metrics | grep composite_dra
```

### RBAC

The Helm chart ClusterRole includes events permissions (`create`, `patch` on `events` in core API group) so the driver can emit K8s Events on ResourceClaims in any namespace. No additional RBAC setup is needed.

### Helm Values Reference

| Value | Default | Description |
|-------|---------|-------------|
| `metrics.enabled` | `true` | Enable `/metrics` endpoint and metrics port |
| `metrics.port` | `8080` | Metrics HTTP port (plaintext) |
| `metrics.serviceMonitor.enabled` | `false` | Create ServiceMonitor CRs for Prometheus Operator |
| `metrics.serviceMonitor.interval` | `30s` | Scrape interval |

## Kubernetes Events

Events are emitted on composite ResourceClaims during the prepare/unprepare lifecycle:

| Event | Type | When |
|-------|------|------|
| `PrepareStarted` | Normal | Shadow claim creation beginning |
| `PrepareCompleted` | Normal | All shadow claims + gRPC done (includes timing) |
| `PrepareFailed` | Warning | Which driver/device failed |
| `UnprepareCompleted` | Normal | Cleanup done |

```bash
# View events on a specific claim
kubectl describe resourceclaim <name> -n <namespace>

# Filter events cluster-wide
kubectl get events --field-selector reason=PrepareCompleted
kubectl get events --field-selector reason=PrepareFailed
```

## Structured Logging

All log output uses `klog.InfoS`/`ErrorS` with key-value pairs. Common fields: `claim`, `namespace`, `node`, `driver`, `device`, `pool`, `composition`, `count`, `uid`.

Enable JSON output for log aggregation (fluentd, vector, Loki):

```bash
# In Helm values or container args:
--logging-format=json
```

## Limitations

- Gauge metrics (`claims_active`, `shadow_claims_active`, `synthesis_devices_total`) reset to zero on pod restart. Prometheus handles counter resets via `resets()`; for gauges, expect a brief dip after driver pod restarts.
- Metrics port (8080) is plaintext HTTP. If your security policy requires TLS on all ports, use a sidecar proxy (e.g., kube-rbac-proxy).
- `synthesis_duration_seconds` records the full pairer+publisher pipeline time. When multiple compositions exist, each gets the same duration value (single `recompute()` call processes all compositions).
- Event emission requires the driver ServiceAccount to have `events` create/patch permissions in the claim's namespace. The Helm chart ClusterRole includes this by default.

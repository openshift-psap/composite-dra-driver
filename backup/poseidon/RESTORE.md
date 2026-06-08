# Poseidon Webhook Restore Instructions

**Backed up:** 2026-06-03
**Helm release:** `dra` in `dra-webhook-system` namespace
**Chart:** `dra-admission-webhook-0.1.0` (image tag: `pr-20`)

## Quick Restore

```bash
export KUBECONFIG=/home/thibrahi/kubeconfigs/kubeconfig_files/poseidon

# Re-install from the same chart + values
helm install dra \
  /home/thibrahi/workspace/refs_repos/dra-rail-admission-webhook/charts/dra-admission-webhook \
  -n dra-webhook-system \
  -f /home/thibrahi/workspace/dra-webhook/composite-dra-driver/backup/poseidon/helm-values.yaml
```

## What Was Running

- 1x webhook pod (`dra-gpu-nic-webhook-658bbf46dd-9tfgw`)
- 1x reconciler pod (`dra-gpu-nic-webhook-reconciler-86c65969b4-nrjlj`)
- MutatingWebhookConfiguration: `dra-gpu-nic-webhook` (2 webhooks: /mutate + /mutate-ext)
- TLS: helm-generated, secret `dra-gpu-nic-webhook-tls`
- Config: 8 rails (10.0.x - 10.7.x), maxPairsPerNUMA=4, maxPairsPerNode=8, ethernet mode

## Backup Files

- `helm-values.yaml` — full computed Helm values
- `helm-manifest.yaml` — rendered Helm manifest
- `dra-webhook-system-full.yaml` — full namespace dump (pods, services, configmaps, secrets)
- `resourceslices.yaml` — all ResourceSlices (nvidia + dranet)
- `deviceclasses.yaml` — all DeviceClasses
- `resourceclaims.yaml` — all ResourceClaims

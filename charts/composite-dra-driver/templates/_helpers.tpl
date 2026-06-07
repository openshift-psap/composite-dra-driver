{{- define "composite-dra.fullname" -}}
composite-dra-driver
{{- end -}}

{{- define "composite-dra.labels" -}}
app.kubernetes.io/name: {{ include "composite-dra.fullname" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "composite-dra.selectorLabels" -}}
app.kubernetes.io/name: {{ include "composite-dra.fullname" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Backwards-compat guard: fail if old webhook.enabled key is used.
*/}}
{{- define "composite-dra.validateWebhookConfig" -}}
{{- if hasKey .Values.webhook "enabled" -}}
{{- fail "webhook.enabled has been replaced by webhook.mode ('auto'|'enabled'|'disabled'). Please update your values file." -}}
{{- end -}}
{{- end -}}

{{/*
Determine if webhook should be deployed.
Returns "true" or "false" based on webhook.mode and K8s version.
*/}}
{{- define "composite-dra.webhookNeeded" -}}
{{- include "composite-dra.validateWebhookConfig" . -}}
{{- if eq .Values.webhook.mode "enabled" -}}
true
{{- else if eq .Values.webhook.mode "disabled" -}}
false
{{- else -}}
{{- if semverCompare "<1.36-0" .Capabilities.KubeVersion.Version -}}
true
{{- else -}}
false
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Generate TLS certificate data for helm-generated mode.
Returns JSON dict with keys: ca, crt, key.
Uses lookup to preserve existing certs across helm upgrades.
*/}}
{{- define "composite-dra.tlsCerts" -}}
{{- $ns := .Values.namespace -}}
{{- $secretName := printf "%s-webhook-tls" (include "composite-dra.fullname" .) -}}
{{- $svcName := printf "%s-webhook" (include "composite-dra.fullname" .) -}}
{{- $cn := printf "%s.%s.svc" $svcName $ns -}}
{{- $altNames := list $cn (printf "%s.%s.svc.cluster.local" $svcName $ns) $svcName (printf "%s.%s" $svcName $ns) -}}
{{- $existing := lookup "v1" "Secret" $ns $secretName -}}
{{- if $existing -}}
  {{- $caCert := index $existing.data "ca.crt" | b64dec -}}
  {{- $tlsCert := index $existing.data "tls.crt" | b64dec -}}
  {{- $tlsKey := index $existing.data "tls.key" | b64dec -}}
  {{- dict "ca" $caCert "crt" $tlsCert "key" $tlsKey | toJson -}}
{{- else -}}
  {{- $ca := genCA "composite-dra-webhook-ca" 365 -}}
  {{- $cert := genSignedCert $cn nil $altNames 365 $ca -}}
  {{- dict "ca" $ca.Cert "crt" $cert.Cert "key" $cert.Key | toJson -}}
{{- end -}}
{{- end -}}

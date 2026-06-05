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

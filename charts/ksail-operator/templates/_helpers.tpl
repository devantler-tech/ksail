{{/* Chart name, optionally overridden. */}}
{{- define "ksail-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Fully qualified app name. */}}
{{- define "ksail-operator.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "ksail-operator.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/* Common labels. */}}
{{- define "ksail-operator.labels" -}}
app.kubernetes.io/name: {{ include "ksail-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{/* Selector labels for the operator. */}}
{{- define "ksail-operator.operatorSelectorLabels" -}}
app.kubernetes.io/name: {{ include "ksail-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: operator
{{- end -}}

{{/* Selector labels for the UI. */}}
{{- define "ksail-operator.uiSelectorLabels" -}}
app.kubernetes.io/name: {{ include "ksail-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: ui
{{- end -}}

{{/* ServiceAccount name. */}}
{{- define "ksail-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "ksail-operator.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/* Operator container image. */}}
{{- define "ksail-operator.operatorImage" -}}
{{- $tag := .Values.operator.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.operator.image.repository $tag -}}
{{- end -}}

{{/* UI container image. */}}
{{- define "ksail-operator.uiImage" -}}
{{- $tag := .Values.ui.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.ui.image.repository $tag -}}
{{- end -}}

{{/* Name of the Secret holding the OIDC client and session secrets. */}}
{{- define "ksail-operator.oidc.secretName" -}}
{{- if .Values.auth.oidc.existingSecret -}}
{{- .Values.auth.oidc.existingSecret -}}
{{- else -}}
{{- printf "%s-oidc" (include "ksail-operator.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/* OIDC redirect (callback) URL: the explicit value, or derived from the first ingress host.
     The callback is served by the operator REST API under /api, so it points at /api/v1/auth/callback. */}}
{{- define "ksail-operator.oidc.redirectURL" -}}
{{- if .Values.auth.oidc.redirectURL -}}
{{- .Values.auth.oidc.redirectURL -}}
{{- else -}}
{{- $host := (first .Values.ui.ingress.hosts).host -}}
{{- $scheme := ternary "https" "http" (gt (len .Values.ui.ingress.tls) 0) -}}
{{- printf "%s://%s/api/v1/auth/callback" $scheme $host -}}
{{- end -}}
{{- end -}}

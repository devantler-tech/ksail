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

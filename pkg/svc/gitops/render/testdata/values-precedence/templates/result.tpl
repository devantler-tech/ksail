apiVersion: v1
kind: ConfigMap
metadata:
  name: values-probe
data:
  replicaCount: {{ .Values.replicaCount | toString | quote }}
  values.json: {{ .Values | toJson | quote }}

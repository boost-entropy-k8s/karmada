{{- define "karmada.crd.patch.webhook.clusterresourcebinding" -}}
{{ $name :=  include "karmada.name" .}}
{{ $namespace := include "karmada.namespace" .}}
---
# The following patch enables conversion webhook for CRD
# CRD conversion requires k8s 1.13 or later.
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: clusterresourcebindings.work.karmada.io
  {{- if "karmada.commonLabels" }}
  labels:
    {{- include "karmada.commonLabels" . | nindent 4 }}
  {{- end }}
spec:
  conversion:
    strategy: Webhook
    webhook:
      clientConfig:
        url: https://{{ $name }}-webhook.{{ $namespace }}.svc:443/convert
        {{- include "karmada.webhook.caBundle" . | nindent 8 }}
      conversionReviewVersions: ["v1"]
---
{{- end -}}

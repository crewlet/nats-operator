{{/*
Expand the name of the chart.
*/}}
{{- define "nats-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "nats-operator.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart name and version as used by the helm.sh/chart label.
*/}}
{{- define "nats-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels stamped on every resource the chart renders.
*/}}
{{- define "nats-operator.labels" -}}
helm.sh/chart: {{ include "nats-operator.chart" . }}
{{ include "nats-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: nats-operator
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels — the immutable subset used by Deployment / Service /
ServiceMonitor / PDB selectors. Must never change for a given release.
*/}}
{{- define "nats-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nats-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{/*
Name of the ServiceAccount the manager runs as.
*/}}
{{- define "nats-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (printf "%s-controller-manager" (include "nats-operator.fullname" .)) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Namespace the chart installs into. Defaults to .Release.Namespace.
*/}}
{{- define "nats-operator.namespace" -}}
{{- if .Values.namespaceOverride }}
{{- .Values.namespaceOverride }}
{{- else }}
{{- .Release.Namespace }}
{{- end }}
{{- end }}

{{/*
Image reference. Honors digest first, then tag, then chart appVersion.
*/}}
{{- define "nats-operator.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- if .Values.image.digest -}}
{{ .Values.image.repository }}@{{ .Values.image.digest }}
{{- else -}}
{{ .Values.image.repository }}:{{ $tag }}
{{- end -}}
{{- end }}

{{/*
Manager command-line arguments. The chart computes leader-election,
health-probe, metrics and nack-integration flags from values, then
appends extraArgs.
*/}}
{{- define "nats-operator.args" -}}
{{- $args := list -}}
{{- if .Values.leaderElection.enabled -}}
{{- $args = append $args "--leader-elect" -}}
{{- end -}}
{{- if .Values.healthProbeBindAddress -}}
{{- $args = append $args (printf "--health-probe-bind-address=%s" .Values.healthProbeBindAddress) -}}
{{- end -}}
{{- if .Values.metrics.enabled -}}
{{- $args = append $args (printf "--metrics-bind-address=%s" .Values.metrics.bindAddress) -}}
{{- if not .Values.metrics.secure -}}
{{- $args = append $args "--metrics-secure=false" -}}
{{- end -}}
{{- end -}}
{{- if .Values.nackIntegration -}}
{{- $mode := .Values.nackIntegration -}}
{{- if not (has $mode (list "auto" "enabled" "disabled")) -}}
{{- fail (printf "nackIntegration must be one of auto, enabled, disabled; got %q" $mode) -}}
{{- end -}}
{{- $args = append $args (printf "--nack-integration=%s" $mode) -}}
{{- end -}}
{{- range .Values.extraArgs -}}
{{- $args = append $args . -}}
{{- end -}}
{{- toYaml $args -}}
{{- end }}

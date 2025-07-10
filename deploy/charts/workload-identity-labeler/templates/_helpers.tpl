{{- define "workload-identity-labeler.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
*/}}
{{- define "workload-identity-labeler.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create the name of the service account to use
*/}}
{{- define "workload-identity-labeler.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
    {{ default (include "workload-identity-labeler.fullname" .) .Values.serviceAccount.name }}
{{- else -}}
    {{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{/*
Namespace for all resources to be installed into
If not defined in values file then the helm release namespace is used
By default this is not set so the helm release namespace will be used
*/}}
{{- define "workload-identity-labeler.namespace" -}}
    {{ .Values.namespace | default .Release.Namespace }}
{{- end -}}


{{/*
Labels to add to the workload-identity-labeler resources.
These labels are added to all resources created by this chart.
*/}}
{{- define "workload-identity-labeler.labels" -}}
    app: {{ include "workload-identity-labeler.fullname" . }}
    app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
    app.kubernetes.io/name: {{ include "workload-identity-labeler.fullname" . }}
    app.kubernetes.io/instance: {{ .Release.Name | quote }}
    app.kubernetes.io/managed-by: {{ .Release.Service | quote }}
    helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
    {{- with .Values.global.labels }}
        {{- toYaml . | nindent 4 }}
    {{- end }}
{{- end -}}
{{/*
Expand the name of the chart.
*/}}
{{- define "pca.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name. Truncated at 63 chars to fit
DNS-1123 + leave room for `-postgres`/`-redis` child suffixes.
*/}}
{{- define "pca.fullname" -}}
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

{{- define "pca.serverFullname" -}}
{{- printf "%s-server" (include "pca.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "pca.postgresFullname" -}}
{{- printf "%s-postgres" (include "pca.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "pca.redisFullname" -}}
{{- printf "%s-redis" (include "pca.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "pca.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels for every chart-managed object.
*/}}
{{- define "pca.labels" -}}
helm.sh/chart: {{ include "pca.chart" . }}
{{ include "pca.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "pca.selectorLabels" -}}
app.kubernetes.io/name: {{ include "pca.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "pca.serverSelectorLabels" -}}
{{ include "pca.selectorLabels" . }}
app.kubernetes.io/component: server
{{- end -}}

{{- define "pca.postgresSelectorLabels" -}}
{{ include "pca.selectorLabels" . }}
app.kubernetes.io/component: postgres
{{- end -}}

{{- define "pca.redisSelectorLabels" -}}
{{ include "pca.selectorLabels" . }}
app.kubernetes.io/component: redis
{{- end -}}

{{/*
Server ServiceAccount name. Either chart-managed or user-supplied.
*/}}
{{- define "pca.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "pca.serverFullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Name of the Secret server reads env from. Honors secrets.existing override.
*/}}
{{- define "pca.secretName" -}}
{{- if .Values.secrets.existing -}}
{{- .Values.secrets.existing -}}
{{- else -}}
{{- printf "%s-secret" (include "pca.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Postgres DSN resolved from chart-managed PG or postgres.externalDsn.
*/}}
{{- define "pca.postgresDsn" -}}
{{- if .Values.postgres.externalDsn -}}
{{- .Values.postgres.externalDsn -}}
{{- else if .Values.postgres.enabled -}}
{{- printf "postgres://app:$(PCA_DB_PASSWORD)@%s:5432/app?sslmode=disable" (include "pca.postgresFullname" .) -}}
{{- else -}}
{{- fail "postgres.enabled=false and postgres.externalDsn is empty — set one" -}}
{{- end -}}
{{- end -}}

{{/*
Redis address resolved from chart-managed Redis or redis.externalAddr.
*/}}
{{- define "pca.redisAddr" -}}
{{- if .Values.redis.externalAddr -}}
{{- .Values.redis.externalAddr -}}
{{- else if .Values.redis.enabled -}}
{{- printf "%s:6379" (include "pca.redisFullname" .) -}}
{{- else -}}
{{- fail "redis.enabled=false and redis.externalAddr is empty — set one" -}}
{{- end -}}
{{- end -}}

{{/*
Hard precondition checks. Render-time failures keep mis-configured installs
out of the cluster instead of letting them limp along until first sandbox.
*/}}
{{- define "pca.assertions" -}}
{{- if and (not .Values.secrets.existing) (not .Values.secrets.jwtSecret) -}}
{{- fail "secrets.jwtSecret is required (>=32 chars) when secrets.existing is empty" -}}
{{- end -}}
{{- if and .Values.secrets.jwtSecret (lt (len .Values.secrets.jwtSecret) 32) -}}
{{- fail "secrets.jwtSecret must be at least 32 characters" -}}
{{- end -}}
{{- if ne .Values.config.sandbox.k8s.namespace .Values.rbac.sandboxNamespace -}}
{{- fail (printf "config.sandbox.k8s.namespace=%s must equal rbac.sandboxNamespace=%s" .Values.config.sandbox.k8s.namespace .Values.rbac.sandboxNamespace) -}}
{{- end -}}
{{- if not (has .Values.sandbox.network (list "internal" "bridge" "none")) -}}
{{- fail (printf "sandbox.network=%s must be one of internal|bridge|none" .Values.sandbox.network) -}}
{{- end -}}
{{- if not (has .Values.config.sandbox.driver (list "docker" "k8s")) -}}
{{- fail (printf "config.sandbox.driver=%s must be docker|k8s" .Values.config.sandbox.driver) -}}
{{- end -}}
{{- end -}}

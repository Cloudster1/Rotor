{{/* Names and labels, as Exciter's chart has them. */}}

{{- define "rotor.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "rotor.fullname" -}}
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

{{- define "rotor.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "rotor.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: rotor
{{- end -}}

{{- define "rotor.selectorLabels" -}}
app.kubernetes.io/name: {{ include "rotor.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "rotor.image" -}}
{{- $img := index .root.Values.images .name -}}
{{- if $img.digest -}}
{{- printf "%s@%s" $img.repository $img.digest -}}
{{- else -}}
{{- printf "%s:%s" $img.repository (default .root.Chart.AppVersion $img.tag) -}}
{{- end -}}
{{- end -}}

{{/* The sandbox namespace, the api's Service as sandboxes dial it, and the control plane's accounts. */}}

{{- define "rotor.sandboxNamespace" -}}
{{- default (printf "%s-sandboxes" .Release.Namespace) .Values.sandbox.namespace -}}
{{- end -}}

{{- define "rotor.runnerHost" -}}
{{- printf "%s-api.%s.svc" (include "rotor.fullname" .) .Release.Namespace -}}
{{- end -}}

{{- define "rotor.apiAccount" -}}{{ include "rotor.fullname" . }}-api{{- end -}}
{{- define "rotor.workerAccount" -}}{{ include "rotor.fullname" . }}-worker{{- end -}}
{{- define "rotor.jobsAccount" -}}{{ include "rotor.fullname" . }}-jobs{{- end -}}

{{/* Where the database is: the bundled one names its own Service. */}}
{{- define "rotor.dbHost" -}}
{{- if .Values.postgresql.enabled -}}
{{- printf "%s-postgresql" (include "rotor.fullname" .) -}}
{{- else -}}
{{- required "Set database.host, or enable the bundled postgresql for a trial: the chart does not guess where the database is." .Values.database.host -}}
{{- end -}}
{{- end -}}

{{/* The basic-auth Secrets holding one password each, made by the secrets Job or named in secrets.names. */}}
{{- define "rotor.credentialSecret" -}}
{{- $suffix := dict "dbOwner" "db-owner" "dbApp" "db-app" "dbAdmin" "db-admin" "valkey" "valkey" -}}
{{- default (printf "%s-%s" (include "rotor.fullname" .root) (get $suffix .name)) (get .root.Values.secrets.names .name) -}}
{{- end -}}

{{/* A password as an env var the URLs below expand with $(NAME); Kubernetes expands only those declared earlier. */}}
{{- define "rotor.passwordEnv" -}}
- name: {{ .var }}
  valueFrom:
    secretKeyRef:
      name: {{ include "rotor.credentialSecret" (dict "root" .root "name" .name) }}
      key: password
{{- end -}}

{{/* Generated passwords are alphanumeric, so they need no escaping here. */}}
{{- define "rotor.dbURL" -}}
{{- $v := .root.Values.database -}}
{{- printf "postgres://%s:$(%s)@%s:%d/%s?sslmode=%s" .role .var (include "rotor.dbHost" .root) (int $v.port) $v.name $v.sslmode -}}
{{- end -}}

{{- define "rotor.valkeyHost" -}}
{{- if .Values.valkey.bundled -}}
{{- printf "%s-valkey" (include "rotor.fullname" .) -}}
{{- else -}}
{{- .Values.valkey.host -}}
{{- end -}}
{{- end -}}

{{- define "rotor.valkeyAuth" -}}
{{- if or .Values.valkey.bundled .Values.valkey.auth -}}true{{- end -}}
{{- end -}}

{{- define "rotor.valkeyScheme" -}}
{{- if and .Values.valkey.tls (not .Values.valkey.bundled) -}}rediss{{- else -}}redis{{- end -}}
{{- end -}}

{{- define "rotor.appURL" -}}
{{- printf "%s://%s" (ternary "https" "http" .Values.ingress.tls.enabled) .Values.ingress.host -}}
{{- end -}}

{{/* public=reachable pairs from a map, as ROTOR_*_BACKCHANNEL reads them. */}}
{{- define "rotor.rewrites" -}}
{{- $pairs := list -}}
{{- range $public, $reachable := . -}}
{{- $pairs = append $pairs (printf "%s=%s" $public $reachable) -}}
{{- end -}}
{{- join "," $pairs -}}
{{- end -}}

{{/* Annotations that order a hook the same way under Helm and under Argo CD. */}}
{{- define "rotor.hookAnnotations" -}}
helm.sh/hook: {{ .helm }}
helm.sh/hook-weight: {{ .weight | quote }}
helm.sh/hook-delete-policy: before-hook-creation
argocd.argoproj.io/hook: {{ .argo }}
argocd.argoproj.io/hook-delete-policy: BeforeHookCreation
argocd.argoproj.io/sync-wave: {{ .weight | quote }}
{{- end -}}

{{/* What a release may not be, refused before anything is applied. */}}
{{- define "rotor.check" -}}
{{- if and (eq .Values.env "production") .Values.sandbox.allowRunc -}}
{{- fail "sandbox.allowRunc is true with env production, which would run hostile code without gVisor. Turn it off and name the cluster's gVisor RuntimeClass in sandbox.runtimeClass." -}}
{{- end -}}
{{- if and (not .Values.sandbox.runtimeClass) (not .Values.sandbox.allowRunc) -}}
{{- fail "sandbox.runtimeClass is empty, so sandboxes would run without gVisor. Name the cluster's gVisor RuntimeClass, or set sandbox.allowRunc for a test cluster that has none." -}}
{{- end -}}
{{- if not .Values.openbao.addr -}}
{{- fail "Set openbao.addr: Rotor keeps every refresh token, provider key and run token in OpenBao, and its runner certificate comes from there." -}}
{{- end -}}
{{- if and (ne .Values.env "development") (not .Values.oidc.issuer) -}}
{{- fail "Set oidc.issuer and oidc.clientSecret: outside development signing in through the realm is the only way in." -}}
{{- end -}}
{{- if and .Values.oidc.issuer (not .Values.oidc.clientSecret.name) -}}
{{- fail "oidc.issuer is set, so name the Secret holding Rotor's client secret in oidc.clientSecret.name." -}}
{{- end -}}
{{- if and (ne .Values.env "development") .Values.valkey.host (not (include "rotor.valkeyAuth" .)) -}}
{{- fail "valkey.auth is off, so anyone who reaches Valkey could read the tools' catalogs and wake every api replica. Give Valkey a password, name its Secret in secrets.names.valkey and set valkey.auth, or use the bundled one for a trial." -}}
{{- end -}}
{{- if and .Values.openbao.configure.enabled (not .Values.openbao.configure.tokenSecret.name) -}}
{{- fail "openbao.configure is on, so name the Secret holding a token allowed to configure OpenBao in openbao.configure.tokenSecret.name." -}}
{{- end -}}
{{- end -}}

{{/*
The environment the api and the worker share, built once so a setting cannot reach one and not the other.
Credentials are not here; rotor.secretEnv hands them over by reference.
*/}}
{{- define "rotor.env" -}}
{{- include "rotor.check" . -}}
ROTOR_ENV: {{ .Values.env | quote }}
ROTOR_LOG_LEVEL: {{ .Values.logLevel | quote }}
ROTOR_APP_URL: {{ include "rotor.appURL" . | quote }}
ROTOR_HTTP_ADDR: {{ printf ":%d" (int .Values.api.port) | quote }}
ROTOR_SECURE_COOKIES: {{ .Values.ingress.tls.enabled | quote }}
{{- with .Values.api.trustedProxies }}
ROTOR_TRUSTED_PROXIES: {{ join "," . | quote }}
{{- end }}
{{- with .Values.api.oauthFetchAllow }}
ROTOR_OAUTH_FETCH_ALLOW: {{ join "," . | quote }}
{{- end }}
{{- with .Values.oidc.issuer }}
ROTOR_OIDC_ISSUER: {{ . | quote }}
ROTOR_OIDC_CLIENT_ID: {{ $.Values.oidc.clientId | quote }}
{{- end }}
{{- with include "rotor.rewrites" .Values.oidc.backchannel }}
ROTOR_OIDC_BACKCHANNEL: {{ . | quote }}
{{- end }}
ROTOR_OPENBAO_ADDR: {{ .Values.openbao.addr | quote }}
ROTOR_OPENBAO_AUTH: {{ .Values.openbao.auth | quote }}
ROTOR_OPENBAO_ROLE: {{ .Values.openbao.role | quote }}
ROTOR_OPENBAO_MOUNT: {{ .Values.openbao.mount | quote }}
{{- with include "rotor.rewrites" .Values.tools.backchannel }}
ROTOR_TOOLS_BACKCHANNEL: {{ . | quote }}
{{- end }}
ROTOR_SANDBOX_BACKEND: kubernetes
ROTOR_SANDBOX_IMAGE: {{ include "rotor.image" (dict "root" . "name" "runner") | quote }}
ROTOR_SANDBOX_STACK: {{ .Release.Name | quote }}
ROTOR_SANDBOX_NAMESPACE: {{ include "rotor.sandboxNamespace" . | quote }}
ROTOR_SANDBOX_RUNTIME_CLASS: {{ .Values.sandbox.runtimeClass | quote }}
ROTOR_SANDBOX_SERVICE_ACCOUNT: {{ .Values.sandbox.serviceAccount | quote }}
ROTOR_SANDBOX_ALLOW_RUNC: {{ .Values.sandbox.allowRunc | quote }}
ROTOR_RUNNER_ADDR: {{ printf ":%d" (int .Values.api.runnerPort) | quote }}
ROTOR_RUNNER_HOST: {{ include "rotor.runnerHost" . | quote }}
ROTOR_RUNNER_DIAL: {{ printf "%s:%d" (include "rotor.runnerHost" .) (int .Values.api.runnerPort) | quote }}
ROTOR_WORKER_METRICS_ADDR: {{ printf ":%d" (int .Values.worker.metricsPort) | quote }}
ROTOR_API_METRICS_ADDR: {{ printf ":%d" (int .Values.api.metricsPort) | quote }}
{{- with .Values.telemetry.otlpEndpoint }}
ROTOR_OTLP_ENDPOINT: {{ . | quote }}
{{- end }}
ROTOR_TRACE_SAMPLE_RATIO: {{ .Values.telemetry.sampleRatio | quote }}
ROTOR_LANGSMITH_ENDPOINT: {{ .Values.telemetry.langsmithEndpoint | quote }}
{{- end -}}

{{/* The credentials the api and the worker read, by reference to their Secrets. */}}
{{- define "rotor.secretEnv" -}}
{{ include "rotor.passwordEnv" (dict "root" . "name" "dbApp" "var" "DB_APP_PASSWORD") }}
{{ include "rotor.passwordEnv" (dict "root" . "name" "dbAdmin" "var" "DB_ADMIN_PASSWORD") }}
- name: ROTOR_DB_URL
  value: {{ include "rotor.dbURL" (dict "root" . "role" "rotor_app" "var" "DB_APP_PASSWORD") | quote }}
- name: ROTOR_DB_ADMIN_URL
  value: {{ include "rotor.dbURL" (dict "root" . "role" "rotor_admin" "var" "DB_ADMIN_PASSWORD") | quote }}
{{- with .Values.oidc.clientSecret.name }}
- name: ROTOR_OIDC_CLIENT_SECRET
  valueFrom:
    secretKeyRef: { name: {{ . }}, key: {{ $.Values.oidc.clientSecret.key }} }
{{- end }}
{{- $host := include "rotor.valkeyHost" . }}
{{- if $host }}
{{- if include "rotor.valkeyAuth" . }}
{{ include "rotor.passwordEnv" (dict "root" . "name" "valkey" "var" "VALKEY_PASSWORD") }}
- name: ROTOR_VALKEY_URL
  value: {{ printf "%s://:$(VALKEY_PASSWORD)@%s:%d/%d" (include "rotor.valkeyScheme" .) $host (int .Values.valkey.port) (int .Values.valkey.db) | quote }}
{{- else }}
- name: ROTOR_VALKEY_URL
  value: {{ printf "%s://%s:%d/%d" (include "rotor.valkeyScheme" .) $host (int .Values.valkey.port) (int .Values.valkey.db) | quote }}
{{- end }}
{{- end }}
{{- end -}}

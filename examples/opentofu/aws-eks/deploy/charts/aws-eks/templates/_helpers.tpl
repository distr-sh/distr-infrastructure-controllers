{{- define "byoc.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "byoc.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "byoc.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
The instance name, validated like the OpenTofu controller does.
*/}}
{{- define "byoc.instanceName" -}}
{{- $name := required "instanceName is required" .Values.instanceName }}
{{- if not (regexMatch "^[a-z0-9]([a-z0-9-]{1,38}[a-z0-9])$" $name) }}
{{- fail "instanceName must be 3 to 40 lowercase letters, digits and hyphens, starting and ending with a letter or digit" }}
{{- end }}
{{- if or (not (regexMatch "^[a-z]" $name)) (contains "--" $name) }}
{{- fail "instanceName must start with a letter and must not contain two hyphens in a row, because Amazon RDS names the database after it" }}
{{- end }}
{{- $name }}
{{- end }}

{{/*
The data of the Secret: the variables, the secret environment and the passphrase.
*/}}
{{- define "byoc.secretData" -}}
{{- range $name, $value := .Values.variables }}
TF_VAR_{{ $name }}: {{ (kindIs "string" $value | ternary $value (toJson $value)) | b64enc | quote }}
{{- end }}
{{- range $name, $value := .Values.secretEnv }}
{{ $name }}: {{ toString $value | b64enc | quote }}
{{- end }}
{{- with .Values.stateEncryptionPassphrase }}
{{- if lt (len .) 16 }}
{{- fail "stateEncryptionPassphrase must have at least 16 characters" }}
{{- end }}
STATE_ENCRYPTION_PASSPHRASE: {{ . | b64enc | quote }}
{{- end }}
{{- end }}

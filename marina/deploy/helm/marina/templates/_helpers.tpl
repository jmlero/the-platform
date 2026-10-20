{{- define "marina.labels" -}}
app.kubernetes.io/name: marina
app.kubernetes.io/part-of: the-platform
{{- end -}}

{{- define "marina.image" -}}
{{- if .Values.image.digest -}}
{{ .Values.image.repository }}@{{ .Values.image.digest }}
{{- else -}}
{{ .Values.image.repository }}:{{ .Values.image.tag | default .Chart.AppVersion }}
{{- end -}}
{{- end -}}

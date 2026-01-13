{{ define "packages" -}}

{{- range $idx, $val := .packages -}}
{{/* Special handling for config */}}
  {{- if .IsMain -}}
---
title: "{{ .Title }}"
linkTitle: "{{ .Title }}"
weight: 2
description: "Generated API reference documentation for {{ if ne .GroupName "" -}} {{ .DisplayName }}{{ else -}} Konflux Operator{{- end -}}."
---
{{ .GetComment -}}
  {{- end -}}
{{- end }}

# Resource Types {#resource-types}

{{ range .packages -}}
  {{ $isConfig := (eq .GroupName "") }}
  {{- range .VisibleTypes -}}
    {{- if or .IsExported (and $isConfig (eq .DisplayName "Configuration")) }}
- **<a href="{{ .Link }}">{{ .DisplayName }}</a>**
    {{- end -}}
  {{- end -}}
{{- end -}}

{{ range .packages }}
  {{ if ne .GroupName "" -}}
    {{/* For package with a group name, list all type definitions in it. */}}
    {{- range .VisibleTypes }}
      {{- if or .Referenced .IsExported -}}
{{ template "type" . }}
      {{- end -}}
    {{ end }}
  {{ else }}
    {{/* For package w/o group name, list only types referenced. */}}
    {{ $isConfig := (eq .GroupName "") }}
    {{- range .VisibleTypes -}}
      {{- if or .Referenced $isConfig -}}
{{ template "type" . }}
      {{- end -}}
    {{- end }}
  {{- end }}
{{- end }}
{{- end }}

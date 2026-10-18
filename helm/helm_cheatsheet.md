# Helm Template Syntax Cheatsheet
This cheatsheet covers the most commonly used Helm template syntax and functions.

## Basic Syntax
### Access Values:
```Go
{{ .Values.key }}
```
### Default Values:
```Go
{{ default "defaultValue" .Values.key }}
```
### Pipelines:
```Go
{{ .Values.key | default "defaultValue" | quote }}
```
## Control Structures
### If/Else:
```Go
{{ if .Values.condition }}
  # Do something
{{ else if .Values.otherCondition }}
  # Do something else
{{ else }}
  # Default case
{{ end }}
```
### With:
```Go
{{ with .Values.nested }}
  {{ .key }}
{{ end }}
```
### Range:
```Go
{{ range .Values.list }}
  {{ . }}
{{ end }}
```
## Functions
### Include:
```Go
{{ include "template.name" . }}
```
### Required:
```Go
{{ required "Error message" .Values.key }}
```
## Defining and Using Templates
### Define:
```Go
{{ define "template.name" }}
  # Template content
{{ end }}
```
### Use:
```Go
{{ template "template.name" . }}
```
## Comments
### Single Line:
```Go
{{- /* This is a comment */ -}}
```
### Multi-Line:
```Go
{{- /*
  This is a
  multi-line comment
*/ -}}
```
## YAML Specific
### Indentation:
```Go
{{ .Values.key | indent 4 }}
```
Nindent (Newline + Indent):
```Go
{{ .Values.key | nindent 4 }}
```

## Helm CLI (short)

```sh
helm install web ./mychart --dry-run=client --debug    # render only, no cluster changes
helm upgrade --install web ./mychart -f values-prod.yaml --rollback-on-failure --timeout 5m
#   Helm 3: use --atomic instead of --rollback-on-failure
helm history web                                       # revisions of the release
helm rollback web 1                                    # back to revision 1
helm uninstall web
```

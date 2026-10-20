package content

import (
	"html/template"
	"strings"
)

var emailTemplate = template.Must(template.New("email").Parse(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>{{.Subject}}</title></head>
<body style="margin:0;padding:24px;background:#f5f6f8;">
<div style="max-width:600px;margin:0 auto;padding:24px;background:#ffffff;border-radius:8px;font-family:Arial,Helvetica,sans-serif;font-size:16px;line-height:1.5;color:#1f2937;">
{{- range .Paragraphs}}
<p>{{range $i, $line := .}}{{if $i}}<br>{{end}}{{$line}}{{end}}</p>
{{- end}}
</div>
</body>
</html>
`))

// EmailHTML wraps a rendered plain-text email in a simple HTML document
// (DECISIONS.md D15): the text is escaped, blank lines separate paragraphs
// and single line breaks become <br>. The model never writes HTML.
func EmailHTML(subject, body string) string {
	body = strings.ReplaceAll(strings.TrimSpace(body), "\r\n", "\n")
	var paragraphs [][]string
	for _, p := range strings.Split(body, "\n\n") {
		if p = strings.TrimSpace(p); p != "" {
			paragraphs = append(paragraphs, strings.Split(p, "\n"))
		}
	}
	var b strings.Builder
	// The template is fixed and its data are strings, so it cannot fail.
	_ = emailTemplate.Execute(&b, struct {
		Subject    string
		Paragraphs [][]string
	}{subject, paragraphs})
	return b.String()
}

package webconfig

import (
	"embed"
	"html/template"
)

//go:embed templates/*.html
var templateFiles embed.FS

func init() {
	parsed := template.Must(template.New("web-config").Funcs(template.FuncMap{
		"dict": func(values ...any) map[string]any {
			result := make(map[string]any, len(values)/2)
			for i := 0; i+1 < len(values); i += 2 {
				key, ok := values[i].(string)
				if ok {
					result[key] = values[i+1]
				}
			}
			return result
		},
	}).ParseFS(templateFiles, "templates/*.html"))
	ginTemplate = parsed
}

var ginTemplate *template.Template

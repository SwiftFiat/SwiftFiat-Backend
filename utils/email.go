package utils

import (
	"bytes"
	"fmt"
	"html/template"
)

// RenderEmailTemplate parses and executes an HTML template with the provided data.
func RenderEmailTemplate(templatePath string, data any) (string, error) {
	tpl, err := template.ParseFiles(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}
	var tplBody bytes.Buffer
	if err := tpl.Execute(&tplBody, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}
	return tplBody.String(), nil
}

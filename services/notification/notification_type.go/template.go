package notification_type

import (
	"fmt"
	"strings"
	"text/template"
)

func getTemplate(data interface{}, templateName string) (*strings.Builder, error) {
	// Parse the HTML template file
	tmpl, err := template.ParseFiles("static/templates/" + templateName)
	if err != nil {
		return nil, fmt.Errorf("error parsing template: %v", err)
	}

	// Buffer to hold the rendered template
	var body strings.Builder

	// Execute the template with data and write the result to the buffer
	err = tmpl.Execute(&body, data)
	if err != nil {
		return nil, fmt.Errorf("error executing template: %v", err)
	}

	return &body, nil
}

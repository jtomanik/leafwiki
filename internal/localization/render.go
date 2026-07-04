package localization

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"text/template"

	"github.com/nicksnyder/go-i18n/v2/i18n"
)

type Result struct {
	Message string
	Missing bool
	Err     error
}

var ErrLocalizationTemplateDataMismatch = errors.New("localization template data mismatch")

type TemplateDataMismatchError struct {
	MessageID CatalogMessageID
}

func (e *TemplateDataMismatchError) Error() string {
	if e == nil {
		return ErrLocalizationTemplateDataMismatch.Error()
	}
	return fmt.Sprintf("%s for %s", ErrLocalizationTemplateDataMismatch, e.MessageID)
}

func (e *TemplateDataMismatchError) Unwrap() error {
	return ErrLocalizationTemplateDataMismatch
}

func (r *Renderer) Render(id any, defaultEnglish string, args ...string) Result {
	messageID := messageIDString(id)
	data := positionalTemplateData(args)
	if r == nil || messageID == "" {
		return Result{Message: renderFallback(defaultEnglish, data)}
	}
	result, err := r.localizer.Localize(&i18n.LocalizeConfig{
		MessageID:    messageID,
		TemplateData: data,
	})
	if err == nil && r.hasCatalogID(messageID) && !strings.Contains(result, "<no value>") {
		return Result{Message: result}
	}
	if err == nil && strings.Contains(result, "<no value>") {
		err = &TemplateDataMismatchError{MessageID: CatalogMessageID(messageID)}
	}
	var missingErr *i18n.MessageNotFoundErr
	fallback := renderFallback(defaultEnglish, data)
	return Result{
		Message: fallback,
		Missing: errors.As(err, &missingErr) || !r.hasCatalogID(messageID),
		Err:     err,
	}
}

func messageIDString(id any) string {
	switch v := id.(type) {
	case string:
		return v
	case interface{ String() string }:
		return v.String()
	default:
		return fmt.Sprint(v)
	}
}

func positionalTemplateData(args []string) map[string]any {
	data := make(map[string]any, len(args))
	for i, arg := range args {
		data[fmt.Sprintf("Arg%d", i)] = arg
	}
	return data
}

func renderFallback(defaultEnglish string, data map[string]any) string {
	tmpl, err := template.New("fallback").Option("missingkey=error").Parse(defaultEnglish)
	if err != nil {
		return defaultEnglish
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return defaultEnglish
	}
	return out.String()
}

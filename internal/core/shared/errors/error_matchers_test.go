package errors_test

import (
	"fmt"

	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

type renderedFieldErrorExpectation struct {
	Field     string
	Code      sharederrors.FieldErrorCode
	MessageID sharederrors.MessageID
	Message   string
}

func MatchRenderedFieldError(expected renderedFieldErrorExpectation) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual *sharederrors.FieldError) (bool, error) {
		if actual == nil {
			return false, nil
		}
		return actual.Field == expected.Field &&
			actual.Code == expected.Code &&
			actual.MessageID == expected.MessageID &&
			actual.Message == expected.Message, nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} match rendered field error\n{{format .Data 1}}", expected)
}

func MatchValidationErrorContract() types.GomegaMatcher {
	expected := sharederrors.NewValidationErrors()
	return gcustom.MakeMatcher(func(actual *sharederrors.ValidationErrors) (bool, error) {
		if actual == nil {
			return false, nil
		}
		return actual.Error() == expected.Error(), nil
	}).WithMessage("match the validation error contract")
}

type localizedRenderingExpectation struct {
	MessageID sharederrors.MessageID
	Message   string
	Template  string
}

type localizedErrorTextExpectation struct {
	MessageID sharederrors.MessageID
	Text      string
	Cause     error
}

func HaveLocalizedErrorText(expected localizedErrorTextExpectation) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual error) (bool, error) {
		if actual == nil {
			return false, nil
		}
		localized, ok := sharederrors.AsLocalizedError(actual)
		if !ok {
			return false, nil
		}
		return localized.MessageID == expected.MessageID &&
			actual.Error() == localizedErrorText(expected), nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} have localized error text\n{{format .Data 1}}", expected)
}

func localizedErrorText(expected localizedErrorTextExpectation) string {
	if expected.Cause == nil {
		return expected.Text
	}
	return fmt.Sprintf("%s: %s", expected.Text, expected.Cause.Error())
}

func HaveLocalizedRendering(expected localizedRenderingExpectation) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(actual any) (bool, error) {
		got, ok, err := renderedLocalizedFields(actual)
		if err != nil || !ok {
			return false, err
		}
		return got == expected, nil
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} have localized rendering\n{{format .Data 1}}", expected)
}

func renderedLocalizedFields(actual any) (localizedRenderingExpectation, bool, error) {
	switch value := actual.(type) {
	case *sharederrors.LocalizedError:
		if value == nil {
			return localizedRenderingExpectation{}, false, nil
		}
		return localizedRenderingExpectation{
			MessageID: value.MessageID,
			Message:   value.Message,
			Template:  value.Template,
		}, true, nil
	case sharederrors.LocalizedErrorDetail:
		return localizedRenderingExpectation{
			MessageID: value.MessageID,
			Message:   value.Message,
			Template:  value.Template,
		}, true, nil
	case *sharederrors.LocalizedErrorDetail:
		if value == nil {
			return localizedRenderingExpectation{}, false, nil
		}
		return localizedRenderingExpectation{
			MessageID: value.MessageID,
			Message:   value.Message,
			Template:  value.Template,
		}, true, nil
	default:
		return localizedRenderingExpectation{}, false, fmt.Errorf("HaveLocalizedRendering expects a localized error or detail")
	}
}

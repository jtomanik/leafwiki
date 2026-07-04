package errors_test

import (
	"fmt"

	. "github.com/onsi/gomega"
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
	return WithTransform(renderedFieldErrorFields, Equal(expected))
}

func MatchValidationErrors(errorsMatcher types.GomegaMatcher) types.GomegaMatcher {
	return WithTransform(validationFieldErrors, errorsMatcher)
}

type localizedRenderingExpectation struct {
	MessageID sharederrors.MessageID
	Message   string
	Template  string
}

type nilLocalizedErrorContract struct {
	RenderedText string
	WrappedCause error
}

func HaveLocalizedRendering(expected localizedRenderingExpectation) types.GomegaMatcher {
	return WithTransform(renderedLocalizedFields, Equal(expected))
}

func HaveNilLocalizedErrorContract() types.GomegaMatcher {
	return WithTransform(nilLocalizedErrorFields, Equal(nilLocalizedErrorContract{}))
}

func renderedFieldErrorFields(actual *sharederrors.FieldError) (renderedFieldErrorExpectation, error) {
	if actual == nil {
		return renderedFieldErrorExpectation{}, fmt.Errorf("MatchRenderedFieldError expects a non-nil field error")
	}
	return renderedFieldErrorExpectation{
		Field:     actual.Field,
		Code:      actual.Code,
		MessageID: actual.MessageID,
		Message:   actual.Message,
	}, nil
}

func validationFieldErrors(actual *sharederrors.ValidationErrors) ([]*sharederrors.FieldError, error) {
	if actual == nil {
		return nil, fmt.Errorf("MatchValidationErrors expects a non-nil validation error collection")
	}
	return actual.Errors, nil
}

func nilLocalizedErrorFields(actual *sharederrors.LocalizedError) nilLocalizedErrorContract {
	return nilLocalizedErrorContract{
		RenderedText: actual.Error(),
		WrappedCause: actual.Unwrap(),
	}
}

func renderedLocalizedFields(actual any) (localizedRenderingExpectation, error) {
	switch value := actual.(type) {
	case *sharederrors.LocalizedError:
		if value == nil {
			return localizedRenderingExpectation{}, fmt.Errorf("HaveLocalizedRendering expects a non-nil localized error")
		}
		return localizedRenderingExpectation{
			MessageID: value.MessageID,
			Message:   value.Message,
			Template:  value.Template,
		}, nil
	case sharederrors.LocalizedErrorDetail:
		return localizedRenderingExpectation{
			MessageID: value.MessageID,
			Message:   value.Message,
			Template:  value.Template,
		}, nil
	case *sharederrors.LocalizedErrorDetail:
		if value == nil {
			return localizedRenderingExpectation{}, fmt.Errorf("HaveLocalizedRendering expects a non-nil localized error detail")
		}
		return localizedRenderingExpectation{
			MessageID: value.MessageID,
			Message:   value.Message,
			Template:  value.Template,
		}, nil
	default:
		return localizedRenderingExpectation{}, fmt.Errorf("HaveLocalizedRendering expects a localized error or detail")
	}
}

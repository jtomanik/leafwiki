package branding

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

type brandingValidationField string

const brandingSiteNameValidationField brandingValidationField = "siteName"

func (field brandingValidationField) String() string {
	return string(field)
}

func HaveBrandingStructuredError(status int, code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.HaveHTTPStructuredError(status, code, sharederrors.MessageIDForCode(code))
}

type brandingValidationErrorObservation struct {
	Status int
	Error  string
	Fields []*sharederrors.FieldError
}

func HaveBrandingValidationError(
	field brandingValidationField,
	code sharederrors.FieldErrorCode,
	messageID sharederrors.MessageID,
) types.GomegaMatcher {
	return gomega.WithTransform(observeBrandingValidationError, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Status": gomega.Equal(http.StatusBadRequest),
		"Error":  gomega.Equal(brandingValidationErrorCode),
		"Fields": testmatchers.ContainFieldError(testmatchers.ValidationFieldName(field.String()), code, messageID),
	}))
}

func observeBrandingValidationError(rec *httptest.ResponseRecorder) brandingValidationErrorObservation {
	var body struct {
		Error  string                     `json:"error"`
		Fields []*sharederrors.FieldError `json:"fields"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return brandingValidationErrorObservation{
		Status: rec.Code,
		Error:  body.Error,
		Fields: body.Fields,
	}
}

func MatchBrandingLocalizedError(code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.MatchLocalizedError(code, sharederrors.MessageIDForCode(code))
}

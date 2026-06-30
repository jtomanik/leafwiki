package branding

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/onsi/gomega/gcustom"
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

type brandingValidationErrorExpectation struct {
	Field     brandingValidationField
	Code      sharederrors.FieldErrorCode
	MessageID sharederrors.MessageID
}

func HaveBrandingValidationError(
	field brandingValidationField,
	code sharederrors.FieldErrorCode,
	messageID sharederrors.MessageID,
) types.GomegaMatcher {
	expected := brandingValidationErrorExpectation{Field: field, Code: code, MessageID: messageID}
	return gcustom.MakeMatcher(func(rec *httptest.ResponseRecorder) (bool, error) {
		if rec.Code != http.StatusBadRequest {
			return false, nil
		}
		var body struct {
			Error  string                     `json:"error"`
			Fields []*sharederrors.FieldError `json:"fields"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			return false, fmt.Errorf("decode branding validation error response: %w", err)
		}
		if body.Error != brandingValidationErrorCode {
			return false, nil
		}
		matched, err := testmatchers.ContainFieldError(testmatchers.ValidationFieldName(field.String()), code, messageID).Match(body.Fields)
		return matched, err
	}).WithTemplate("Expected:\n{{.FormattedActual}}\n{{.To}} have branding validation error\n{{format .Data 1}}", expected)
}

func MatchBrandingLocalizedError(code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.MatchLocalizedError(code, sharederrors.MessageIDForCode(code))
}

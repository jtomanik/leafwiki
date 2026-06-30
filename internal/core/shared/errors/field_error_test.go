package errors_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

const (
	testAuthEmailInvalidFieldCode sharederrors.FieldErrorCode = "auth_email_invalid"
	testAuthEmailInvalidMessageID sharederrors.MessageID      = "validation.auth.email_invalid"
	testSiteNameRequiredFallback  string                      = "site name is required"
	testEmailValidationField      string                      = "email"
	testSiteNameValidationField   string                      = "siteName"
	testSlugValidationField       string                      = "slug"
)

var _ = Describe("field validation errors", func() {
	It("TestValidationErrorsAddWithCodeSerializesStableFieldContract", func() {
		validation := sharederrors.NewValidationErrors()
		validation.AddWithCode(
			testSlugValidationField,
			testAuthEmailInvalidFieldCode,
			testAuthEmailInvalidMessageID,
		)

		encoded, err := json.Marshal(validation)

		Expect(err).NotTo(HaveOccurred())
		Expect(validation).To(testmatchers.ContainFieldError(testmatchers.ValidationFieldName(testSlugValidationField), testAuthEmailInvalidFieldCode, testAuthEmailInvalidMessageID))
		Expect(string(encoded)).To(MatchJSON(fieldErrorsJSON(validation)))
	})

	It("TestValidationErrorsLegacyAddRendersCatalogBackedDefaultCode", func() {
		validation := sharederrors.NewValidationErrors()
		validation.Add(testSiteNameValidationField, testSiteNameRequiredFallback)

		Expect(validation.Errors).To(HaveLen(1))
		field := validation.Errors[0]
		Expect(field).To(MatchRenderedFieldError(renderedFieldErrorExpectation{
			Field:     testSiteNameValidationField,
			Code:      sharederrors.FieldValidationErrorCode,
			MessageID: sharederrors.FieldValidationErrorMessageID,
			Message:   renderedMessage(sharederrors.FieldValidationErrorMessageID, testSiteNameRequiredFallback),
		}))
	})

	It("TestValidationErrorsAddWithCodeRendersFromCatalog", func() {
		validation := sharederrors.NewValidationErrors()
		validation.AddWithCode(
			testEmailValidationField,
			testAuthEmailInvalidFieldCode,
			testAuthEmailInvalidMessageID,
		)

		field := validation.Errors[0]
		Expect(field).To(MatchRenderedFieldError(renderedFieldErrorExpectation{
			Field:     testEmailValidationField,
			Code:      testAuthEmailInvalidFieldCode,
			MessageID: testAuthEmailInvalidMessageID,
			Message:   renderedMessage(testAuthEmailInvalidMessageID, ""),
		}))
	})
})

var _ = Describe("field validation edge coverage", func() {
	It("ValidationErrors reports empty and populated state", func() {
		validation := sharederrors.NewValidationErrors()

		Expect(validation.HasErrors()).To(BeFalse())
		Expect(validation).To(MatchValidationErrorContract())

		validation.Add(testSiteNameValidationField, testSiteNameRequiredFallback)
		Expect(validation.HasErrors()).To(BeTrue())
	})
})

func fieldErrorsJSON(validation *sharederrors.ValidationErrors) string {
	raw, err := json.Marshal(validation)
	Expect(err).NotTo(HaveOccurred())
	return string(raw)
}

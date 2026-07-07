package errors_test

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var (
	testAuthEmailInvalidFieldCode sharederrors.FieldErrorCode = newFixtureFieldErrorCode("auth_email_invalid")
	testAuthEmailInvalidMessageID sharederrors.MessageID      = newFixtureMessageID("validation.auth.email_invalid")
)

const (
	testSiteNameRequiredFallback string = "site name is required"
	testEmailValidationField     string = "email"
	testSiteNameValidationField  string = "siteName"
	testSlugValidationField      string = "slug"
)

var _ = Describe("field validation errors", Label("unit"), func() {
	It("serializes explicit field codes with stable field and message identifiers", func() {
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

	It("renders legacy field additions with the catalog-backed validation code", func() {
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

	It("renders explicit field codes through the localization catalog", func() {
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

var _ = Describe("field validation state", Label("unit"), func() {
	It("tracks empty and populated field-error collections", func() {
		validation := sharederrors.NewValidationErrors()

		Expect(validation).To(HaveValidationErrorContract(validationFieldCollectionEmpty))
		Expect(validation).To(MatchValidationErrors(BeEmpty()))

		validation.Add(testSiteNameValidationField, testSiteNameRequiredFallback)
		Expect(validation).To(HaveValidationErrorContract(validationFieldCollectionPopulated))
		Expect(validation).To(MatchValidationErrors(ConsistOf(MatchRenderedFieldError(renderedFieldErrorExpectation{
			Field:     testSiteNameValidationField,
			Code:      sharederrors.FieldValidationErrorCode,
			MessageID: sharederrors.FieldValidationErrorMessageID,
			Message:   renderedMessage(sharederrors.FieldValidationErrorMessageID, testSiteNameRequiredFallback),
		}))))
	})
})

func fieldErrorsJSON(validation *sharederrors.ValidationErrors) string {
	raw, err := json.Marshal(validation)
	Expect(err).NotTo(HaveOccurred())
	return string(raw)
}

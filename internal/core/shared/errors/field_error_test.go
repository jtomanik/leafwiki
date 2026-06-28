package errors

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("field validation errors", func() {
	It("TestValidationErrorsAddWithCodeSerializesStableFieldContract", func() {
		validation := NewValidationErrors()
		validation.AddWithCode(
			"slug",
			"auth_email_invalid",
			"validation.auth.email_invalid",
		)

		encoded, err := json.Marshal(validation)

		Expect(err).NotTo(HaveOccurred())
		Expect(string(encoded)).To(Equal(`{"fields":[{"field":"slug","code":"auth_email_invalid","messageId":"validation.auth.email_invalid","message":"Email is not valid"}]}`))
	})

	It("TestValidationErrorsLegacyAddRendersCatalogBackedDefaultCode", func() {
		validation := NewValidationErrors()
		validation.Add("siteName", "site name is required")

		Expect(validation.Errors).To(HaveLen(1))
		field := validation.Errors[0]
		Expect(field.Code).To(Equal(FieldValidationErrorCode))
		Expect(field.MessageID).To(Equal(FieldValidationErrorMessageID))
		Expect(field.Field).To(Equal("siteName"))
		Expect(field.Message).To(Equal("Validation error"))
	})

	It("TestValidationErrorsAddWithCodeRendersFromCatalog", func() {
		validation := NewValidationErrors()
		validation.AddWithCode(
			"email",
			"auth_email_invalid",
			"validation.auth.email_invalid",
		)

		field := validation.Errors[0]
		Expect(field.Message).To(Equal("Email is not valid"))
		Expect(field.Code).To(Equal(FieldErrorCode("auth_email_invalid")))
		Expect(field.MessageID).To(Equal(MessageID("validation.auth.email_invalid")))
	})
})

var _ = Describe("field validation edge coverage", func() {
	It("ValidationErrors reports empty and populated state", func() {
		validation := NewValidationErrors()

		Expect(validation.HasErrors()).To(BeFalse())
		Expect(validation.Error()).To(Equal("validation error"))

		validation.Add("siteName", "site name is required")
		Expect(validation.HasErrors()).To(BeTrue())
	})

	It("typed field error codes stringify to their stable value", func() {
		Expect(FieldErrorCode("auth_email_invalid").String()).To(Equal("auth_email_invalid"))
	})
})

package errors

import (
	"encoding/json"
	stderrors "errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const (
	testPageVersionConflictCode      ErrorCode = "page_version_conflict"
	testPageVersionConflictMessageID MessageID = "errors.page.version_conflict"
	testAuthInvalidCredentialsCode   ErrorCode = "auth_invalid_credentials"
	testAuthInvalidCredentialsMsgID  MessageID = "errors.auth.invalid_credentials"
)

var _ = Describe("localized errors", func() {
	It("TestNewDefinedLocalizedErrorExposesTypedCodeAndMessageID", func() {
		cause := stderrors.New("storage failed")
		definition := ErrorDefinition{
			Code:      testPageVersionConflictCode,
			MessageID: testPageVersionConflictMessageID,
			Message:   "Page was changed by another request",
			Template:  "page was changed by another request",
		}

		err := NewDefinedLocalizedError(definition, cause, "page-1")

		Expect(err.Code).To(Equal(testPageVersionConflictCode))
		Expect(err.MessageID).To(Equal(testPageVersionConflictMessageID))
		Expect(err.Message).To(Equal("Page was changed by another request"))
		Expect(err.Template).To(Equal("page was changed by another request"))
		Expect(stderrors.Is(err, cause)).To(BeTrue())
		Expect(err.Args).To(Equal([]string{"page-1"}))
	})

	It("TestNewLocalizedErrorKeepsLegacyConstructorButAddsDefaultMessageID", func() {
		err := NewLocalizedError(testAuthInvalidCredentialsCode, "Invalid credentials", "invalid credentials", nil)

		Expect(err.Code).To(Equal(testAuthInvalidCredentialsCode))
		Expect(err.MessageID).To(Equal(testAuthInvalidCredentialsMsgID))
	})

	It("TestNewLocalizedErrorFromCodeRendersCatalogMessage", func() {
		cause := stderrors.New("storage failed")

		err := NewLocalizedErrorFromCode(testPageVersionConflictCode, cause, "docs.md", "README.md")

		Expect(err.Code).To(Equal(testPageVersionConflictCode))
		Expect(err.MessageID).To(Equal(testPageVersionConflictMessageID))
		Expect(err.Message).To(Equal("Page docs.md was changed by another request before README.md could be saved."))
		Expect(err.Template).To(Equal(err.Message))
		Expect(err.Args).To(Equal([]string{"docs.md", "README.md"}))
		Expect(stderrors.Is(err, cause)).To(BeTrue())
	})

	It("TestLocalizedErrorDetailSerializesMessageIDWithCompatibilityFields", func() {
		detail := NewLocalizedErrorDetail(
			testPageVersionConflictCode,
			"Page was changed by another request",
			"page was changed by another request",
			"page-1",
		)

		encoded, err := json.Marshal(detail)

		Expect(err).NotTo(HaveOccurred())
		Expect(string(encoded)).To(Equal(`{"code":"page_version_conflict","messageId":"errors.page.version_conflict","message":"Page was changed by another request","template":"page was changed by another request","args":["page-1"]}`))
	})

	It("TestLocalizedErrorDetailRendersMessageFromCatalog", func() {
		detail := NewLocalizedErrorDetail(
			testAuthInvalidCredentialsCode,
			"legacy fallback",
			"legacy fallback",
		)

		Expect(detail.Message).To(Equal("Invalid credentials"))
		Expect(detail.Template).To(Equal("legacy fallback"))
		Expect(detail.MessageID).To(Equal(testAuthInvalidCredentialsMsgID))
	})

	It("TestLocalizedErrorDetailUsesArgNBridgeAndPreservesArgs", func() {
		err := NewDefinedLocalizedError(ErrorDefinition{
			Code:      testPageVersionConflictCode,
			MessageID: testPageVersionConflictMessageID,
			Message:   "legacy fallback",
			Template:  "page %s could not be saved before %s",
		}, nil, "docs.md", "README.md")

		detail := LocalizedErrorDetailFromError(err)

		Expect(detail.Message).To(Equal("Page docs.md was changed by another request before README.md could be saved."))
		Expect(detail.Args).To(Equal([]string{"docs.md", "README.md"}))
		Expect(detail.Template).To(Equal("page %s could not be saved before %s"))
	})
})

var _ = Describe("localized error edge coverage", func() {
	It("MessageIDForCode handles empty, un-namespaced, and namespaced codes", func() {
		Expect(MessageIDForCode("")).To(BeEmpty())
		Expect(MessageIDForCode("  ")).To(BeEmpty())
		Expect(MessageIDForCode("unknown")).To(Equal(MessageID("errors.unknown")))
		Expect(MessageIDForCode("auth_invalid_credentials")).To(Equal(MessageID("errors.auth.invalid_credentials")))
	})

	It("typed error and message IDs stringify to their stable values", func() {
		Expect(ErrorCode("page_version_conflict").String()).To(Equal("page_version_conflict"))
		Expect(MessageID("errors.page.version_conflict").String()).To(Equal("errors.page.version_conflict"))
	})

	It("nil localized errors have empty Error text and no wrapped cause", func() {
		var err *LocalizedError

		Expect(err.Error()).To(BeEmpty())
		Expect(err.Unwrap()).To(BeNil())
	})

	It("localized errors include the cause in Error text when present", func() {
		err := NewLocalizedError("test_code", "visible message", "visible message", stderrors.New("root cause"))

		Expect(err.Error()).To(Equal("visible message: root cause"))
	})

	It("NewLocalizedErrorFromCodeWithFallback renders using fallback when no catalog entry exists", func() {
		cause := stderrors.New("cause")

		err := NewLocalizedErrorFromCodeWithFallback("custom_missing", "fallback {{.Arg0}}", "fallback template", cause, "value")

		Expect(err.Code).To(Equal(ErrorCode("custom_missing")))
		Expect(err.MessageID).To(Equal(MessageID("errors.custom.missing")))
		Expect(err.Message).To(Equal("fallback value"))
		Expect(err.Template).To(Equal("fallback template"))
		Expect(err.Args).To(Equal([]string{"value"}))
		Expect(stderrors.Is(err, cause)).To(BeTrue())
	})

	It("NewDefinedLocalizedError derives message ID when omitted", func() {
		err := NewDefinedLocalizedError(ErrorDefinition{
			Code:     testAuthInvalidCredentialsCode,
			Message:  "Invalid credentials",
			Template: "invalid credentials",
		}, nil)

		Expect(err.MessageID).To(Equal(testAuthInvalidCredentialsMsgID))
	})

	It("NewLocalizedErrorDetailFromCode renders catalog message and preserves args", func() {
		detail := NewLocalizedErrorDetailFromCode(testPageVersionConflictCode, "docs.md", "README.md")

		Expect(detail.MessageID).To(Equal(testPageVersionConflictMessageID))
		Expect(detail.Message).To(Equal("Page docs.md was changed by another request before README.md could be saved."))
		Expect(detail.Template).To(Equal(detail.Message))
		Expect(detail.Args).To(Equal([]string{"docs.md", "README.md"}))
	})

	It("LocalizedErrorDetailFromError returns an empty detail for nil errors", func() {
		Expect(LocalizedErrorDetailFromError(nil)).To(Equal(LocalizedErrorDetail{}))
	})

	It("LocalizedErrorDetailFromError derives message ID when the error omitted it", func() {
		err := &LocalizedError{
			Code:     testAuthInvalidCredentialsCode,
			Message:  "fallback",
			Template: "fallback",
		}

		detail := LocalizedErrorDetailFromError(err)

		Expect(detail.MessageID).To(Equal(testAuthInvalidCredentialsMsgID))
		Expect(detail.Message).To(Equal("Invalid credentials"))
	})

	It("AsLocalizedError recognizes wrapped localized errors and rejects ordinary errors", func() {
		localized := NewLocalizedError(testAuthInvalidCredentialsCode, "Invalid credentials", "invalid credentials", nil)
		wrapped := fmt.Errorf("wrap: %w", localized)

		got, ok := AsLocalizedError(wrapped)
		Expect(ok).To(BeTrue())
		Expect(got).To(BeIdenticalTo(localized))

		got, ok = AsLocalizedError(stderrors.New("plain"))
		Expect(ok).To(BeFalse())
		Expect(got).To(BeNil())
	})
})

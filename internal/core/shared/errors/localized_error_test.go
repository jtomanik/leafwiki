package errors_test

import (
	"encoding/json"
	stderrors "errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/localization"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

const (
	testPageVersionConflictCode        sharederrors.ErrorCode = "page_version_conflict"
	testPageVersionConflictMessageID   sharederrors.MessageID = "errors.page.version_conflict"
	testAuthInvalidCredentialsCode     sharederrors.ErrorCode = "auth_invalid_credentials"
	testAuthInvalidCredentialsMsgID    sharederrors.MessageID = "errors.auth.invalid_credentials"
	testUnknownMessageID               sharederrors.MessageID = "errors.unknown"
	testCustomMissingCode              sharederrors.ErrorCode = "custom_missing"
	testCustomMissingMessageID         sharederrors.MessageID = "errors.custom.missing"
	testPageVersionConflictFallback    string                 = "page version conflict fallback"
	testAuthInvalidCredentialsFallback string                 = "auth invalid credentials fallback"
	testCustomMissingFallback          string                 = "fallback {{.Arg0}}"
	testCustomMissingTemplate          string                 = "fallback template"
	testDerivedLocalizedFallback       string                 = "fallback"
	testPageVersionConflictTemplate    string                 = "page %s could not be saved before %s"
	testVisibleLocalizedMessage        string                 = "visible message"
)

var _ = Describe("localized errors", func() {
	It("TestNewDefinedLocalizedErrorExposesTypedCodeAndMessageID", func() {
		cause := stderrors.New("storage failed")
		definition := sharederrors.ErrorDefinition{
			Code:      testPageVersionConflictCode,
			MessageID: testPageVersionConflictMessageID,
			Message:   renderedMessage(testPageVersionConflictMessageID, ""),
			Template:  renderedMessage(testPageVersionConflictMessageID, ""),
		}

		err := sharederrors.NewDefinedLocalizedError(definition, cause, "page-1")

		Expect(err).To(testmatchers.MatchLocalizedError(testPageVersionConflictCode, testPageVersionConflictMessageID))
		Expect(err).To(HaveLocalizedRendering(localizedRenderingExpectation{
			MessageID: testPageVersionConflictMessageID,
			Message:   renderedMessage(testPageVersionConflictMessageID, ""),
			Template:  renderedMessage(testPageVersionConflictMessageID, ""),
		}))
		Expect(err).To(MatchError(cause))
		Expect(err.Args).To(Equal([]string{"page-1"}))
	})

	It("TestNewLocalizedErrorKeepsLegacyConstructorButAddsDefaultMessageID", func() {
		err := sharederrors.NewLocalizedError(testAuthInvalidCredentialsCode, testAuthInvalidCredentialsFallback, testAuthInvalidCredentialsFallback, nil)

		Expect(err).To(testmatchers.MatchLocalizedError(testAuthInvalidCredentialsCode, testAuthInvalidCredentialsMsgID))
	})

	It("TestNewLocalizedErrorFromCodeRendersCatalogMessage", func() {
		cause := stderrors.New("storage failed")

		err := sharederrors.NewLocalizedErrorFromCode(testPageVersionConflictCode, cause, "docs.md", "README.md")

		Expect(err).To(testmatchers.MatchLocalizedError(testPageVersionConflictCode, testPageVersionConflictMessageID))
		expectedMessage := renderedMessage(testPageVersionConflictMessageID, "", "docs.md", "README.md")
		Expect(err).To(HaveLocalizedRendering(localizedRenderingExpectation{
			MessageID: testPageVersionConflictMessageID,
			Message:   expectedMessage,
			Template:  expectedMessage,
		}))
		Expect(err.Args).To(Equal([]string{"docs.md", "README.md"}))
		Expect(err).To(MatchError(cause))
	})

	It("TestLocalizedErrorDetailSerializesMessageIDWithCompatibilityFields", func() {
		detail := sharederrors.NewLocalizedErrorDetail(
			testPageVersionConflictCode,
			testPageVersionConflictFallback,
			testPageVersionConflictFallback,
			"page-1",
		)

		encoded, err := json.Marshal(detail)

		Expect(err).NotTo(HaveOccurred())
		Expect(detail).To(testmatchers.HaveStructuredError(testPageVersionConflictCode, testPageVersionConflictMessageID))
		Expect(string(encoded)).To(MatchJSON(localizedDetailJSON(detail)))
	})

	It("TestLocalizedErrorDetailRendersMessageFromCatalog", func() {
		detail := sharederrors.NewLocalizedErrorDetail(
			testAuthInvalidCredentialsCode,
			testAuthInvalidCredentialsFallback,
			testAuthInvalidCredentialsFallback,
		)

		Expect(detail).To(testmatchers.HaveStructuredError(testAuthInvalidCredentialsCode, testAuthInvalidCredentialsMsgID))
		Expect(detail).To(HaveLocalizedRendering(localizedRenderingExpectation{
			MessageID: testAuthInvalidCredentialsMsgID,
			Message:   renderedMessage(testAuthInvalidCredentialsMsgID, testAuthInvalidCredentialsFallback),
			Template:  testAuthInvalidCredentialsFallback,
		}))
	})

	It("TestLocalizedErrorDetailUsesArgNBridgeAndPreservesArgs", func() {
		err := sharederrors.NewDefinedLocalizedError(sharederrors.ErrorDefinition{
			Code:      testPageVersionConflictCode,
			MessageID: testPageVersionConflictMessageID,
			Message:   testPageVersionConflictFallback,
			Template:  testPageVersionConflictTemplate,
		}, nil, "docs.md", "README.md")

		detail := sharederrors.LocalizedErrorDetailFromError(err)

		Expect(detail).To(testmatchers.HaveStructuredError(testPageVersionConflictCode, testPageVersionConflictMessageID))
		Expect(detail).To(HaveLocalizedRendering(localizedRenderingExpectation{
			MessageID: testPageVersionConflictMessageID,
			Message:   renderedMessage(testPageVersionConflictMessageID, testPageVersionConflictFallback, "docs.md", "README.md"),
			Template:  testPageVersionConflictTemplate,
		}))
		Expect(detail.Args).To(Equal([]string{"docs.md", "README.md"}))
	})
})

var _ = Describe("localized error edge coverage", func() {
	It("MessageIDForCode handles empty, un-namespaced, and namespaced codes", func() {
		Expect(sharederrors.MessageIDForCode("")).To(BeEmpty())
		Expect(sharederrors.MessageIDForCode("  ")).To(BeEmpty())
		Expect(sharederrors.MessageIDForCode("unknown")).To(Equal(testUnknownMessageID))
		Expect(sharederrors.MessageIDForCode(testAuthInvalidCredentialsCode)).To(Equal(testAuthInvalidCredentialsMsgID))
	})

	It("nil localized errors have empty Error text and no wrapped cause", func() {
		var err *sharederrors.LocalizedError

		Expect(err.Error()).To(BeEmpty())
		Expect(err.Unwrap()).To(Succeed())
	})

	It("localized errors include the cause in Error text when present", func() {
		cause := stderrors.New("root cause")
		err := sharederrors.NewLocalizedError(testCustomMissingCode, testVisibleLocalizedMessage, testVisibleLocalizedMessage, cause)

		Expect(err).To(MatchError(cause))
		Expect(err).To(HaveLocalizedErrorText(localizedErrorTextExpectation{
			MessageID: testCustomMissingMessageID,
			Text:      testVisibleLocalizedMessage,
			Cause:     cause,
		}))
	})

	It("NewLocalizedErrorFromCodeWithFallback renders using fallback when no catalog entry exists", func() {
		cause := stderrors.New("cause")

		err := sharederrors.NewLocalizedErrorFromCodeWithFallback(testCustomMissingCode, testCustomMissingFallback, testCustomMissingTemplate, cause, "value")

		Expect(err).To(testmatchers.MatchLocalizedError(testCustomMissingCode, testCustomMissingMessageID))
		Expect(err).To(HaveLocalizedRendering(localizedRenderingExpectation{
			MessageID: testCustomMissingMessageID,
			Message:   renderedMessage(testCustomMissingMessageID, testCustomMissingFallback, "value"),
			Template:  testCustomMissingTemplate,
		}))
		Expect(err.Args).To(Equal([]string{"value"}))
		Expect(err).To(MatchError(cause))
	})

	It("NewDefinedLocalizedError derives message ID when omitted", func() {
		err := sharederrors.NewDefinedLocalizedError(sharederrors.ErrorDefinition{
			Code:     testAuthInvalidCredentialsCode,
			Message:  testAuthInvalidCredentialsFallback,
			Template: testAuthInvalidCredentialsFallback,
		}, nil)

		Expect(err).To(testmatchers.MatchLocalizedError(testAuthInvalidCredentialsCode, testAuthInvalidCredentialsMsgID))
	})

	It("NewLocalizedErrorDetailFromCode renders catalog message and preserves args", func() {
		detail := sharederrors.NewLocalizedErrorDetailFromCode(testPageVersionConflictCode, "docs.md", "README.md")

		Expect(detail).To(testmatchers.HaveStructuredError(testPageVersionConflictCode, testPageVersionConflictMessageID))
		expectedMessage := renderedMessage(testPageVersionConflictMessageID, "", "docs.md", "README.md")
		Expect(detail).To(HaveLocalizedRendering(localizedRenderingExpectation{
			MessageID: testPageVersionConflictMessageID,
			Message:   expectedMessage,
			Template:  expectedMessage,
		}))
		Expect(detail.Args).To(Equal([]string{"docs.md", "README.md"}))
	})

	It("LocalizedErrorDetailFromError returns an empty detail for nil errors", func() {
		Expect(sharederrors.LocalizedErrorDetailFromError(nil)).To(Equal(sharederrors.LocalizedErrorDetail{}))
	})

	It("LocalizedErrorDetailFromError derives message ID when the error omitted it", func() {
		err := &sharederrors.LocalizedError{
			Code:     testAuthInvalidCredentialsCode,
			Message:  testDerivedLocalizedFallback,
			Template: testDerivedLocalizedFallback,
		}

		detail := sharederrors.LocalizedErrorDetailFromError(err)

		Expect(detail).To(testmatchers.HaveStructuredError(testAuthInvalidCredentialsCode, testAuthInvalidCredentialsMsgID))
		Expect(detail).To(HaveLocalizedRendering(localizedRenderingExpectation{
			MessageID: testAuthInvalidCredentialsMsgID,
			Message:   renderedMessage(testAuthInvalidCredentialsMsgID, testDerivedLocalizedFallback),
			Template:  testDerivedLocalizedFallback,
		}))
	})

	It("AsLocalizedError recognizes wrapped localized errors and rejects ordinary errors", func() {
		localized := sharederrors.NewLocalizedError(testAuthInvalidCredentialsCode, testAuthInvalidCredentialsFallback, testAuthInvalidCredentialsFallback, nil)
		wrapped := fmt.Errorf("wrap: %w", localized)

		got, ok := sharederrors.AsLocalizedError(wrapped)
		Expect(ok).To(BeTrue())
		Expect(got).To(BeIdenticalTo(localized))

		got, ok = sharederrors.AsLocalizedError(stderrors.New("plain"))
		Expect(ok).To(BeFalse())
		Expect(got).To(BeZero())
	})
})

func renderedMessage(messageID sharederrors.MessageID, defaultEnglish string, args ...string) string {
	return localization.English.Render(messageID, defaultEnglish, args...).Message
}

func localizedDetailJSON(detail sharederrors.LocalizedErrorDetail) string {
	raw, err := json.Marshal(detail)
	Expect(err).NotTo(HaveOccurred())
	return string(raw)
}

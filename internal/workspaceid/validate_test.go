package workspaceid

import (
	"database/sql/driver"
	stderrors "errors"
	"fmt"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("workspace ID parsing", func() {
	It("returns a semantic workspace ID for a valid slug", func() {
		id, err := ParseWorkspaceID("docs-home")

		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(Equal(WorkspaceID("docs-home")))
	})

	It("rejects whitespace input with a typed validation code", func() {
		_, err := ParseWorkspaceID(" Docs ")

		Expect(err).To(Satisfy(func(err error) bool {
			var validationErr *ValidationError
			return stderrors.As(err, &validationErr) && validationErr != nil
		}))
		Expect(err).To(testmatchers.HaveStructuredError(ErrCodeWorkspaceIDWhitespace, sharederrors.MessageIDForCode(ErrCodeWorkspaceIDWhitespace)))
		Expect(WorkspaceIDErrorCode(err)).To(Equal(ErrCodeWorkspaceIDWhitespace))
	})
})

var _ = Describe("workspace ID boundary helpers", func() {
	It("returns stable string forms for transport, URL, and storage boundaries", func() {
		id := WorkspaceID("docs-home")

		Expect(id.HTTPHeaderValue()).To(Equal("docs-home"))
		Expect(id.URLPathSegment()).To(Equal("docs-home"))
		Expect(id.StorageKey()).To(Equal("docs-home"))
	})

	It("path-escapes URL path segments at the boundary", func() {
		id := WorkspaceID("docs home")

		Expect(id.URLPathSegment()).To(Equal("docs%20home"))
	})

	It("ValidateWorkspaceID returns the semantic type", func() {
		id, err := ValidateWorkspaceID("docs-home")

		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(Equal(WorkspaceID("docs-home")))
	})
})

var _ = Describe("workspace ID validation errors", func() {
	It("returns the required code for empty input", func() {
		_, err := ParseWorkspaceID("")

		Expect(WorkspaceIDErrorCode(err)).To(Equal(ErrCodeWorkspaceIDRequired))
		Expect(err).To(testmatchers.HaveStructuredError(ErrCodeWorkspaceIDRequired, sharederrors.MessageIDForCode(ErrCodeWorkspaceIDRequired)))
	})

	It("returns the invalid code for pattern-invalid input", func() {
		_, err := ParseWorkspaceID("Docs")

		Expect(WorkspaceIDErrorCode(err)).To(Equal(ErrCodeWorkspaceIDInvalid))
		Expect(err).To(testmatchers.HaveStructuredError(ErrCodeWorkspaceIDInvalid, sharederrors.MessageIDForCode(ErrCodeWorkspaceIDInvalid)))
	})

	It("returns empty error text for a nil validation error", func() {
		var validationErr *ValidationError

		Expect(validationErr.Error()).To(BeEmpty())
	})

	It("returns no workspace error code for non-validation errors", func() {
		Expect(WorkspaceIDErrorCode(stderrors.New("other error"))).To(BeEmpty())
	})
})

var _ = Describe("workspace ID SQL conversion", func() {
	It("Value returns the string form for a valid workspace ID", func() {
		value, err := WorkspaceID("docs-home").Value()

		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal(driver.Value("docs-home")))
	})

	It("Value returns a typed validation error for an invalid workspace ID", func() {
		value, err := WorkspaceID("Docs").Value()

		Expect(value).To(BeNil())
		Expect(WorkspaceIDErrorCode(err)).To(Equal(ErrCodeWorkspaceIDInvalid))
		Expect(err).To(testmatchers.HaveStructuredError(ErrCodeWorkspaceIDInvalid, sharederrors.MessageIDForCode(ErrCodeWorkspaceIDInvalid)))
	})

	It("Scan accepts string sources", func() {
		var id WorkspaceID

		Expect(id.Scan("docs-home")).To(Succeed())
		Expect(id).To(Equal(WorkspaceID("docs-home")))
	})

	It("Scan accepts byte slice sources", func() {
		var id WorkspaceID

		Expect(id.Scan([]byte("docs-home"))).To(Succeed())
		Expect(id).To(Equal(WorkspaceID("docs-home")))
	})

	It("Scan rejects nil sources with the required code", func() {
		var id WorkspaceID

		err := id.Scan(nil)

		Expect(WorkspaceIDErrorCode(err)).To(Equal(ErrCodeWorkspaceIDRequired))
		Expect(err).To(testmatchers.HaveStructuredError(ErrCodeWorkspaceIDRequired, sharederrors.MessageIDForCode(ErrCodeWorkspaceIDRequired)))
	})

	It("Scan rejects unsupported source types", func() {
		var id WorkspaceID

		err := id.Scan(42)

		Expect(err).To(MatchError(unsupportedWorkspaceIDScanSourceError(42)))
	})
})

func unsupportedWorkspaceIDScanSourceError(value any) error {
	return fmt.Errorf("workspace ID scan source %T is not supported", value)
}

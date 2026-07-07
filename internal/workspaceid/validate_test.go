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

var _ = Describe("workspace ID parsing", Label("unit"), func() {
	It("returns a semantic workspace ID for a valid slug", func() {
		id, err := ParseWorkspaceID("docs-home")

		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(Equal(newFixtureWorkspaceID("docs-home")))
	})

	It("rejects whitespace input with a typed validation code", func() {
		_, err := ParseWorkspaceID(" Docs ")

		Expect(err).To(Satisfy(func(err error) bool {
			var validationErr *ValidationError
			return stderrors.As(err, &validationErr) && validationErr != nil
		}))
		Expect(err).To(testmatchers.HaveStructuredError(ErrCodeWorkspaceIDWhitespace, sharederrors.MessageIDForCode(ErrCodeWorkspaceIDWhitespace)))
	})
})

var _ = Describe("workspace ID boundary helpers", Label("unit"), func() {
	It("returns stable string forms for transport, URL, and storage boundaries", func() {
		fixture := workspaceIDBoundaryFixtureFor("docs-home")

		Expect(workspaceIDBoundaryObservationFor(fixture)).To(Equal(workspaceIDBoundaryObservation{
			ID:         fixture.ID,
			HTTPHeader: workspaceIDBoundaryMatchesCanonicalID,
			URLPath:    workspaceIDBoundaryMatchesCanonicalID,
			StorageKey: workspaceIDBoundaryMatchesCanonicalID,
		}))
	})

	It("path-escapes URL path segments at the boundary", func() {
		id := newFixtureWorkspaceID("docs home")

		Expect(id.URLPathSegment()).To(Equal("docs%20home"))
	})

	It("ValidateWorkspaceID returns the semantic type", func() {
		id, err := ValidateWorkspaceID("docs-home")

		Expect(err).NotTo(HaveOccurred())
		Expect(id).To(Equal(newFixtureWorkspaceID("docs-home")))
	})
})

var _ = Describe("workspace ID validation errors", Label("unit"), func() {
	It("returns the required code for empty input", func() {
		_, err := ParseWorkspaceID("")

		Expect(err).To(testmatchers.HaveStructuredError(ErrCodeWorkspaceIDRequired, sharederrors.MessageIDForCode(ErrCodeWorkspaceIDRequired)))
	})

	It("returns the invalid code for pattern-invalid input", func() {
		_, err := ParseWorkspaceID("Docs")

		Expect(err).To(testmatchers.HaveStructuredError(ErrCodeWorkspaceIDInvalid, sharederrors.MessageIDForCode(ErrCodeWorkspaceIDInvalid)))
		Expect(workspaceValidationCodeObservationFor(err)).To(Equal(workspaceValidationCodeObservation{
			State:     workspaceValidationCodePresent,
			Code:      ErrCodeWorkspaceIDInvalid,
			MessageID: sharederrors.MessageIDForCode(ErrCodeWorkspaceIDInvalid),
		}))
	})

	It("reports no structured workspace validation state for a nil validation error", func() {
		var validationErr *ValidationError

		Expect(workspaceValidationCodeObservationFor(validationErr)).To(Equal(workspaceValidationCodeObservation{
			State: workspaceValidationCodeAbsent,
		}))
	})

	It("returns no workspace error code for non-validation errors", func() {
		Expect(WorkspaceIDErrorCode(stderrors.New("other error"))).To(BeEmpty())
	})
})

var _ = Describe("workspace ID SQL conversion", Label("unit"), func() {
	It("Value returns the string form for a valid workspace ID", func() {
		value, err := newFixtureWorkspaceID("docs-home").Value()

		Expect(err).NotTo(HaveOccurred())
		Expect(value).To(Equal(driver.Value("docs-home")))
	})

	It("Value returns a typed validation error for an invalid workspace ID", func() {
		value, err := newFixtureWorkspaceID("Docs").Value()

		Expect(value).To(BeNil())
		Expect(err).To(testmatchers.HaveStructuredError(ErrCodeWorkspaceIDInvalid, sharederrors.MessageIDForCode(ErrCodeWorkspaceIDInvalid)))
	})

	It("Scan accepts string sources", func() {
		var id WorkspaceID

		Expect(id.Scan("docs-home")).To(Succeed())
		Expect(id).To(Equal(newFixtureWorkspaceID("docs-home")))
	})

	It("Scan accepts byte slice sources", func() {
		var id WorkspaceID

		Expect(id.Scan([]byte("docs-home"))).To(Succeed())
		Expect(id).To(Equal(newFixtureWorkspaceID("docs-home")))
	})

	It("Scan rejects nil sources with the required code", func() {
		var id WorkspaceID

		err := id.Scan(nil)

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

type workspaceIDBoundaryState uint8

const (
	workspaceIDBoundaryUnexpected workspaceIDBoundaryState = iota
	workspaceIDBoundaryMatchesCanonicalID
)

type workspaceIDBoundaryObservation struct {
	ID         WorkspaceID
	HTTPHeader workspaceIDBoundaryState
	URLPath    workspaceIDBoundaryState
	StorageKey workspaceIDBoundaryState
}

type workspaceIDBoundaryFixture struct {
	ID  WorkspaceID
	Raw string
}

type workspaceValidationCodeState uint8

const (
	workspaceValidationCodeAbsent workspaceValidationCodeState = iota
	workspaceValidationCodePresent
)

type workspaceValidationCodeObservation struct {
	State     workspaceValidationCodeState
	Code      sharederrors.ErrorCode
	MessageID sharederrors.MessageID
}

func workspaceIDBoundaryFixtureFor(raw string) workspaceIDBoundaryFixture {
	GinkgoHelper()

	id, err := ParseWorkspaceID(raw)
	Expect(err).To(Succeed())
	return workspaceIDBoundaryFixture{
		ID:  id,
		Raw: raw,
	}
}

func workspaceIDBoundaryObservationFor(fixture workspaceIDBoundaryFixture) workspaceIDBoundaryObservation {
	return workspaceIDBoundaryObservation{
		ID:         fixture.ID,
		HTTPHeader: workspaceIDBoundaryStateFor(fixture.ID.HTTPHeaderValue(), fixture.Raw),
		URLPath:    workspaceIDBoundaryStateFor(fixture.ID.URLPathSegment(), fixture.Raw),
		StorageKey: workspaceIDBoundaryStateFor(fixture.ID.StorageKey(), fixture.Raw),
	}
}

func workspaceIDBoundaryStateFor(value string, raw string) workspaceIDBoundaryState {
	if value == raw {
		return workspaceIDBoundaryMatchesCanonicalID
	}
	return workspaceIDBoundaryUnexpected
}

func workspaceValidationCodeObservationFor(err error) workspaceValidationCodeObservation {
	code := WorkspaceIDErrorCode(err)
	if code == "" {
		return workspaceValidationCodeObservation{State: workspaceValidationCodeAbsent}
	}
	return workspaceValidationCodeObservation{
		State:     workspaceValidationCodePresent,
		Code:      code,
		MessageID: sharederrors.MessageIDForCode(code),
	}
}

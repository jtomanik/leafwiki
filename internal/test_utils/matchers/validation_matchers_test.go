package matchers

import (
	"errors"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

var _ = ginkgo.Describe("semantic validation test matchers", ginkgo.Label("unit"), func() {
	ginkgo.It("matches validation field errors from value slices and reflected containers", func() {
		field := sharederrors.NewFieldErrorWithCode(validationFieldWireValue(testTitleField), sharederrors.FieldValidationErrorCode, sharederrors.FieldValidationErrorMessageID)
		reflected := struct {
			Errors []struct {
				Field     string
				Code      sharederrors.FieldErrorCode
				MessageID sharederrors.MessageID
			}
		}{
			Errors: []struct {
				Field     string
				Code      sharederrors.FieldErrorCode
				MessageID sharederrors.MessageID
			}{{
				Field:     validationFieldWireValue(testTitleField),
				Code:      sharederrors.FieldValidationErrorCode,
				MessageID: sharederrors.FieldValidationErrorMessageID,
			}},
		}

		Expect([]sharederrors.FieldError{*field}).To(ContainFieldError(testTitleField, sharederrors.FieldValidationErrorCode, sharederrors.FieldValidationErrorMessageID))
		Expect([]*sharederrors.FieldError{field}).To(ContainFieldError(testTitleField, sharederrors.FieldValidationErrorCode, sharederrors.FieldValidationErrorMessageID))
		Expect(reflected).To(ContainFieldError(testTitleField, sharederrors.FieldValidationErrorCode, sharederrors.FieldValidationErrorMessageID))
		Expect([]any{field}).NotTo(ContainFieldError(testTitleField, sharederrors.FieldValidationErrorCode, sharederrors.FieldValidationErrorMessageID))
	})

	ginkgo.It("observes validation field extraction for nil and reflected field collections", func() {
		field := sharederrors.NewFieldErrorWithCode(validationFieldWireValue(testTitleField), sharederrors.FieldValidationErrorCode, sharederrors.FieldValidationErrorMessageID)
		reflectedPointers := []*struct {
			Field     string
			Code      sharederrors.FieldErrorCode
			MessageID sharederrors.MessageID
		}{
			nil,
			{
				Field:     validationFieldWireValue(testTitleField),
				Code:      sharederrors.FieldValidationErrorCode,
				MessageID: sharederrors.FieldValidationErrorMessageID,
			},
		}

		Expect((*sharederrors.ValidationErrors)(nil)).To(matchFieldErrorExtraction(matcherRejected, BeNil()))
		Expect(sharederrors.ValidationErrors{Errors: []*sharederrors.FieldError{nil, field}}).To(matchFieldErrorExtraction(
			matcherObserved,
			ConsistOf(fieldErrorData{
				Field:     validationFieldWireValue(testTitleField),
				Code:      fieldErrorCodeWireValue(sharederrors.FieldValidationErrorCode),
				MessageID: messageIDWireValue(sharederrors.FieldValidationErrorMessageID),
			}),
		))
		Expect((*struct{ Errors []sharederrors.FieldError })(nil)).To(matchFieldErrorExtraction(matcherRejected, BeNil()))
		Expect(struct{}{}).To(matchFieldErrorExtraction(matcherRejected, BeNil()))
		Expect(reflectedPointers).To(matchFieldErrorExtraction(
			matcherObserved,
			ConsistOf(fieldErrorData{
				Field:     validationFieldWireValue(testTitleField),
				Code:      fieldErrorCodeWireValue(sharederrors.FieldValidationErrorCode),
				MessageID: messageIDWireValue(sharederrors.FieldValidationErrorMessageID),
			}),
		))
		Expect([]string{"not", "field", "errors"}).To(matchFieldErrorExtraction(matcherRejected, BeNil()))
	})

	ginkgo.It("matches validation issue codes from slices and reflected result shapes", func() {
		issues := []wikivalidation.Issue{{Code: wikivalidation.IssueCodeBrokenLink}}
		reflected := struct {
			Issues []struct {
				Code wikivalidation.IssueCode
			}
		}{
			Issues: []struct {
				Code wikivalidation.IssueCode
			}{{Code: wikivalidation.IssueCodeBrokenLink}},
		}

		Expect(issues).To(HaveValidationIssue(wikivalidation.IssueCodeBrokenLink))
		Expect(reflected).To(HaveValidationIssue(wikivalidation.IssueCodeBrokenLink))
		Expect([]any{wikivalidation.Issue{Code: wikivalidation.IssueCodeBrokenLink}}).NotTo(HaveValidationIssue(wikivalidation.IssueCodeBrokenLink))
	})

	ginkgo.It("observes validation issue extraction across pointer and reflected edge shapes", func() {
		result := &wikivalidation.Result{Issues: []wikivalidation.Issue{{Code: wikivalidation.IssueCodeBrokenLink}}}
		reflectedPointers := []*struct {
			Code wikivalidation.IssueCode
		}{
			nil,
			{Code: ""},
			{Code: wikivalidation.IssueCodeInvalidLink},
		}

		Expect(result).To(matchValidationIssueExtraction(matcherObserved, ConsistOf(issueCodeWireValue(wikivalidation.IssueCodeBrokenLink))))
		Expect((*wikivalidation.Result)(nil)).To(matchValidationIssueExtraction(matcherRejected, BeNil()))
		Expect(reflectedPointers).To(matchValidationIssueExtraction(matcherObserved, ConsistOf(issueCodeWireValue(wikivalidation.IssueCodeInvalidLink))))
		Expect(struct{}{}).To(matchValidationIssueExtraction(matcherRejected, BeNil()))
		Expect([]string{"not", "issues"}).To(matchValidationIssueExtraction(matcherRejected, BeNil()))
		Expect(result).NotTo(HaveValidationIssues(wikivalidation.IssueCodeBrokenLink, wikivalidation.IssueCodeInvalidLink))
		Expect(result).NotTo(HaveValidationIssues(wikivalidation.IssueCodeMissingTitle))
	})

	ginkgo.It("formats semantic matcher failures with the domain verb", func() {
		matcher := HaveErrorCode(testErrorCode)

		Expect(matcher).To(matchMatcherFailureFormat("have error code"))
		Expect(ValidationFieldName(validationFieldWireValue(testTitleField))).To(Equal(testTitleField))
		Expect(testErrorCode).To(HaveErrorCode(testErrorCode))
		Expect(testMessageID).To(HaveMessageID(testMessageID))
		Expect("plain").NotTo(HaveErrorCode(testErrorCode))
		Expect("plain").NotTo(HaveMessageID(testMessageID))
		Expect(errors.New("plain")).NotTo(HaveMCPStructuredError(testErrorCode, testMessageID))
		Expect(struct{}{}).To(matchMatcherAttempt(MatchLocalizedError(testErrorCode, testMessageID), "not an error", matcherErrored))
	})
})

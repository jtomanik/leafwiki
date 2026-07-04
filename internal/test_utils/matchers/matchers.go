package matchers

import (
	"fmt"
	"sort"

	"github.com/onsi/gomega/format"
	"github.com/onsi/gomega/types"

	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

type semanticMatcher struct {
	verb     string
	expected any
	match    func(actual any) (bool, error)
}

func (m semanticMatcher) Match(actual any) (bool, error) {
	return m.match(actual)
}

func (m semanticMatcher) FailureMessage(actual any) string {
	return format.Message(actual, "to "+m.verb, m.expected)
}

func (m semanticMatcher) NegatedFailureMessage(actual any) string {
	return format.Message(actual, "not to "+m.verb, m.expected)
}

func newSemanticMatcher(verb string, expected any, match func(actual any) (bool, error)) types.GomegaMatcher {
	return semanticMatcher{verb: verb, expected: expected, match: match}
}

type structuredErrorExpectation struct {
	Code      sharederrors.ErrorCode
	MessageID sharederrors.MessageID
}

type ValidationField string

func ValidationFieldName(field string) ValidationField {
	return ValidationField(field)
}

func (field ValidationField) String() string {
	return string(field)
}

func HaveStructuredError(code sharederrors.ErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	expected := structuredErrorExpectation{Code: code, MessageID: messageID}
	return newSemanticMatcher("have structured error", expected, func(actual any) (bool, error) {
		data, ok := extractStructuredError(actual)
		if !ok {
			return false, nil
		}
		return data.Code == code.String() && data.MessageID == messageID.String(), nil
	})
}

func HaveMCPStructuredError(code sharederrors.ErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	expected := structuredErrorExpectation{Code: code, MessageID: messageID}
	return newSemanticMatcher("have MCP structured error", expected, func(actual any) (bool, error) {
		data, ok := extractStructuredError(actual)
		if !ok {
			return false, nil
		}
		return data.Code == code.String() && data.MessageID == messageID.String(), nil
	})
}

func HaveErrorCode(code sharederrors.ErrorCode) types.GomegaMatcher {
	return newSemanticMatcher("have error code", code, func(actual any) (bool, error) {
		got, ok := extractErrorCode(actual)
		return ok && got == code.String(), nil
	})
}

func HaveMessageID(messageID sharederrors.MessageID) types.GomegaMatcher {
	return newSemanticMatcher("have message ID", messageID, func(actual any) (bool, error) {
		got, ok := extractMessageID(actual)
		return ok && got == messageID.String(), nil
	})
}

func MatchLocalizedError(code sharederrors.ErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	expected := structuredErrorExpectation{Code: code, MessageID: messageID}
	return newSemanticMatcher("match localized error", expected, func(actual any) (bool, error) {
		err, ok := actual.(error)
		if !ok {
			return false, fmt.Errorf("MatchLocalizedError expects an error value")
		}
		localized, ok := sharederrors.AsLocalizedError(err)
		if !ok {
			return false, nil
		}
		return localized.Code == code && localized.MessageID == messageID, nil
	})
}

func HaveHTTPStructuredError(status int, code sharederrors.ErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	expected := struct {
		Status int
		structuredErrorExpectation
	}{Status: status, structuredErrorExpectation: structuredErrorExpectation{Code: code, MessageID: messageID}}
	return newSemanticMatcher("have HTTP structured error", expected, func(actual any) (bool, error) {
		gotStatus, ok, err := httpStatus(actual)
		if err != nil || !ok || gotStatus != status {
			return false, err
		}
		body, ok, err := httpBody(actual)
		if err != nil || !ok {
			return false, err
		}
		data, ok := extractStructuredError(body)
		if !ok {
			return false, nil
		}
		return data.Code == code.String() && data.MessageID == messageID.String(), nil
	})
}

func ContainFieldError(field ValidationField, code sharederrors.FieldErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	expected := struct {
		Field     ValidationField
		Code      sharederrors.FieldErrorCode
		MessageID sharederrors.MessageID
	}{Field: field, Code: code, MessageID: messageID}
	fieldName := field.String()
	return newSemanticMatcher("contain field error", expected, func(actual any) (bool, error) {
		errors, ok := extractFieldErrors(actual)
		if !ok {
			return false, nil
		}
		for _, got := range errors {
			if got.Field == fieldName && got.Code == code.String() && got.MessageID == messageID.String() {
				return true, nil
			}
		}
		return false, nil
	})
}

func HaveValidationIssue(code wikivalidation.IssueCode) types.GomegaMatcher {
	return newSemanticMatcher("have validation issue", code, func(actual any) (bool, error) {
		codes, ok := extractValidationIssueCodes(actual)
		if !ok {
			return false, nil
		}
		for _, got := range codes {
			if got == code.String() {
				return true, nil
			}
		}
		return false, nil
	})
}

func HaveValidationIssues(codes ...wikivalidation.IssueCode) types.GomegaMatcher {
	expected := append([]wikivalidation.IssueCode(nil), codes...)
	return newSemanticMatcher("have validation issues", expected, func(actual any) (bool, error) {
		got, ok := extractValidationIssueCodes(actual)
		if !ok || len(got) != len(codes) {
			return false, nil
		}
		want := make([]string, 0, len(codes))
		for _, code := range codes {
			want = append(want, code.String())
		}
		sort.Strings(got)
		sort.Strings(want)
		for i := range want {
			if got[i] != want[i] {
				return false, nil
			}
		}
		return true, nil
	})
}

func extractErrorCode(actual any) (string, bool) {
	if code, ok := actual.(sharederrors.ErrorCode); ok {
		return code.String(), true
	}
	data, ok := extractStructuredError(actual)
	return data.Code, ok && data.Code != ""
}

func extractMessageID(actual any) (string, bool) {
	if messageID, ok := actual.(sharederrors.MessageID); ok {
		return messageID.String(), true
	}
	data, ok := extractStructuredError(actual)
	return data.MessageID, ok && data.MessageID != ""
}

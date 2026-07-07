package matchers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

var (
	testErrorCode = newFixtureErrorCode("page_not_found")
	testMessageID = newFixtureMessageID("errors.page.not_found")
)

const (
	testTitleField ValidationField = "title"
	testBodyField  ValidationField = "body"
)

type matcherObservationState string

const (
	matcherObserved matcherObservationState = "observed"
	matcherRejected matcherObservationState = "rejected"
	matcherErrored  matcherObservationState = "errored"
)

type structuredErrorObservation struct {
	State     matcherObservationState
	Code      sharederrors.ErrorCode
	MessageID sharederrors.MessageID
}

type matcherAttemptObservation struct {
	State matcherObservationState
}

type httpBodyObservation struct {
	State matcherObservationState
	Body  string
}

type fieldErrorsObservation struct {
	State  matcherObservationState
	Fields []fieldErrorData
}

type validationIssuesObservation struct {
	State matcherObservationState
	Codes []string
}

type reflectDereferenceObservation struct {
	State matcherObservationState
	Kind  reflect.Kind
}

type matcherFailureFormatObservation struct {
	State matcherObservationState
	Verb  string
}

func newFixtureErrorCode[T ~string](raw T) sharederrors.ErrorCode {
	return sharederrors.ErrorCode(raw)
}

func newFixtureMessageID[T ~string](raw T) sharederrors.MessageID {
	return sharederrors.MessageID(raw)
}

func errorCodeWireValue(code sharederrors.ErrorCode) string {
	return code.String()
}

func messageIDWireValue(messageID sharederrors.MessageID) string {
	return messageID.String()
}

func fieldErrorCodeWireValue(code sharederrors.FieldErrorCode) string {
	return code.String()
}

func validationFieldWireValue(field ValidationField) string {
	return field.String()
}

func issueCodeWireValue(code wikivalidation.IssueCode) string {
	return code.String()
}

func observeStructuredError(actual any) structuredErrorObservation {
	data, ok := extractStructuredError(actual)
	if !ok {
		return structuredErrorObservation{State: matcherRejected}
	}
	return structuredErrorObservation{
		State:     matcherObserved,
		Code:      newFixtureErrorCode(data.Code),
		MessageID: newFixtureMessageID(data.MessageID),
	}
}

func matchStructuredErrorExtraction(state matcherObservationState, code sharederrors.ErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	return WithTransform(observeStructuredError, gstruct.MatchAllFields(gstruct.Fields{
		"State":     Equal(state),
		"Code":      Equal(code),
		"MessageID": Equal(messageID),
	}))
}

func observeStructuredLocalizedError(actual any) structuredErrorObservation {
	data, ok := extractStructuredLocalizedError(actual)
	if !ok {
		return structuredErrorObservation{State: matcherRejected}
	}
	return structuredErrorObservation{
		State:     matcherObserved,
		Code:      newFixtureErrorCode(data.Code),
		MessageID: newFixtureMessageID(data.MessageID),
	}
}

func matchStructuredLocalizedErrorExtraction(state matcherObservationState, code sharederrors.ErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	return WithTransform(observeStructuredLocalizedError, gstruct.MatchAllFields(gstruct.Fields{
		"State":     Equal(state),
		"Code":      Equal(code),
		"MessageID": Equal(messageID),
	}))
}

func observeMatcherAttempt(matcher types.GomegaMatcher, actual any) matcherAttemptObservation {
	matched, err := matcher.Match(actual)
	switch {
	case err != nil:
		return matcherAttemptObservation{State: matcherErrored}
	case matched:
		return matcherAttemptObservation{State: matcherObserved}
	default:
		return matcherAttemptObservation{State: matcherRejected}
	}
}

func matchMatcherAttempt(matcher types.GomegaMatcher, actual any, state matcherObservationState) types.GomegaMatcher {
	return WithTransform(func(struct{}) matcherAttemptObservation {
		return observeMatcherAttempt(matcher, actual)
	}, Equal(matcherAttemptObservation{State: state}))
}

func observeHTTPBody(actual any) httpBodyObservation {
	body, ok, err := httpBody(actual)
	switch {
	case err != nil:
		return httpBodyObservation{State: matcherErrored}
	case ok:
		return httpBodyObservation{State: matcherObserved, Body: body}
	default:
		return httpBodyObservation{State: matcherRejected}
	}
}

func matchHTTPBodyExtraction(state matcherObservationState, body string) types.GomegaMatcher {
	return WithTransform(observeHTTPBody, Equal(httpBodyObservation{
		State: state,
		Body:  body,
	}))
}

func observeFieldErrors(actual any) fieldErrorsObservation {
	fields, ok := extractFieldErrors(actual)
	if !ok {
		return fieldErrorsObservation{State: matcherRejected}
	}
	return fieldErrorsObservation{
		State:  matcherObserved,
		Fields: fields,
	}
}

func matchFieldErrorExtraction(state matcherObservationState, fieldsMatcher types.GomegaMatcher) types.GomegaMatcher {
	return WithTransform(observeFieldErrors, gstruct.MatchAllFields(gstruct.Fields{
		"State":  Equal(state),
		"Fields": fieldsMatcher,
	}))
}

func observeValidationIssues(actual any) validationIssuesObservation {
	codes, ok := extractValidationIssueCodes(actual)
	if !ok {
		return validationIssuesObservation{State: matcherRejected}
	}
	return validationIssuesObservation{
		State: matcherObserved,
		Codes: codes,
	}
}

func matchValidationIssueExtraction(state matcherObservationState, codesMatcher types.GomegaMatcher) types.GomegaMatcher {
	return WithTransform(observeValidationIssues, gstruct.MatchAllFields(gstruct.Fields{
		"State": Equal(state),
		"Codes": codesMatcher,
	}))
}

func observeReflectDereference(actual any) reflectDereferenceObservation {
	value, ok := dereferenceReflectValue(reflect.ValueOf(actual))
	if !ok {
		return reflectDereferenceObservation{State: matcherRejected}
	}
	return reflectDereferenceObservation{
		State: matcherObserved,
		Kind:  value.Kind(),
	}
}

func matchReflectDereference(state matcherObservationState, kind reflect.Kind) types.GomegaMatcher {
	return WithTransform(observeReflectDereference, Equal(reflectDereferenceObservation{
		State: state,
		Kind:  kind,
	}))
}

func observeMatcherFailureFormat(matcher types.GomegaMatcher, actual any) matcherFailureFormatObservation {
	semantic, ok := matcher.(semanticMatcher)
	if !ok {
		return matcherFailureFormatObservation{State: matcherRejected}
	}
	if matcher.FailureMessage(actual) == "" || matcher.NegatedFailureMessage(actual) == "" {
		return matcherFailureFormatObservation{State: matcherRejected, Verb: semantic.verb}
	}
	return matcherFailureFormatObservation{State: matcherObserved, Verb: semantic.verb}
}

func matchMatcherFailureFormat(verb string) types.GomegaMatcher {
	return WithTransform(func(matcher types.GomegaMatcher) matcherFailureFormatObservation {
		return observeMatcherFailureFormat(matcher, errors.New("plain"))
	}, gstruct.MatchAllFields(gstruct.Fields{
		"State": Equal(matcherObserved),
		"Verb":  Equal(verb),
	}))
}

var _ = ginkgo.Describe("semantic test matchers", ginkgo.Label("unit"), func() {
	ginkgo.It("matches localized errors by typed code and message ID", func() {
		err := sharederrors.NewLocalizedErrorFromCode(testErrorCode, errors.New("store unavailable"))

		Expect(err).To(MatchLocalizedError(testErrorCode, testMessageID))
		Expect(err).To(HaveErrorCode(testErrorCode))
		Expect(err).To(HaveMessageID(testMessageID))
		Expect(errors.New("plain")).NotTo(MatchLocalizedError(testErrorCode, testMessageID))
	})

	ginkgo.It("matches structured error payloads without unpacking raw fields", func() {
		detail := sharederrors.NewLocalizedErrorDetailFromCode(testErrorCode)
		payload := map[string]any{
			"error": map[string]any{
				"code":      errorCodeWireValue(testErrorCode),
				"messageId": messageIDWireValue(testMessageID),
				"message":   detail.Message,
			},
		}

		Expect(detail).To(HaveStructuredError(testErrorCode, testMessageID))
		Expect(&detail).To(HaveStructuredError(testErrorCode, testMessageID))
		Expect(payload).To(HaveStructuredError(testErrorCode, testMessageID))
	})

	ginkgo.It("matches HTTP response semantics through response matchers", func() {
		rec := httptest.NewRecorder()
		rec.Header().Set("X-Request-Id", "req-1")
		rec.WriteHeader(http.StatusNotFound)
		detail := sharederrors.NewLocalizedErrorDetailFromCode(testErrorCode)
		payload := map[string]any{"error": detail}
		body, err := json.Marshal(payload)
		Expect(err).To(Succeed())
		_, err = rec.Write(body)
		Expect(err).To(Succeed())

		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))
		Expect(rec).To(HaveHTTPBody(MatchJSON(body)))
		Expect(rec).To(HaveHTTPHeaderWithValue("X-Request-Id", "req-1"))
		Expect(rec).To(HaveHTTPStructuredError(http.StatusNotFound, testErrorCode, testMessageID))
	})

	ginkgo.It("matches field validation errors by typed code and message ID", func() {
		validation := sharederrors.NewValidationErrors()
		validation.AddWithCode(testTitleField.String(), sharederrors.FieldValidationErrorCode, sharederrors.FieldValidationErrorMessageID)

		Expect(validation).To(ContainFieldError(testTitleField, sharederrors.FieldValidationErrorCode, sharederrors.FieldValidationErrorMessageID))
		Expect(validation).NotTo(ContainFieldError(testBodyField, sharederrors.FieldValidationErrorCode, sharederrors.FieldValidationErrorMessageID))
	})

	ginkgo.It("matches markdown validation issues by typed issue codes", func() {
		result := wikivalidation.Result{
			Issues: []wikivalidation.Issue{
				{Code: wikivalidation.IssueCodeBrokenLink},
				{Code: wikivalidation.IssueCodeInvalidLink},
			},
		}

		Expect(result).To(HaveValidationIssue(wikivalidation.IssueCodeBrokenLink))
		Expect(result).To(HaveValidationIssues(wikivalidation.IssueCodeBrokenLink, wikivalidation.IssueCodeInvalidLink))
		Expect(result).NotTo(HaveValidationIssue(wikivalidation.IssueCodeMissingTitle))
	})

	ginkgo.It("matches MCP-style structured errors without knowing protocol fields in specs", func() {
		result := struct {
			Text      string
			Code      string
			MessageID string
			Message   string
		}{
			Text:      "Page not found",
			Code:      errorCodeWireValue(testErrorCode),
			MessageID: messageIDWireValue(testMessageID),
			Message:   "Page not found",
		}

		Expect(result).To(HaveMCPStructuredError(testErrorCode, testMessageID))
	})

	ginkgo.It("extracts structured errors from localized values, string maps, and reflected envelopes", func() {
		localized := sharederrors.NewLocalizedErrorFromCode(testErrorCode, errors.New("store unavailable"))
		detail := sharederrors.NewLocalizedErrorDetailFromCode(testErrorCode)
		stringMap := map[string]string{
			"code":      errorCodeWireValue(testErrorCode),
			"messageID": messageIDWireValue(testMessageID),
		}
		reflected := struct {
			Error struct {
				Code      string
				MessageID string
			}
		}{}
		reflected.Error.Code = errorCodeWireValue(testErrorCode)
		reflected.Error.MessageID = messageIDWireValue(testMessageID)

		Expect(*localized).To(HaveStructuredError(testErrorCode, testMessageID))
		Expect(localized).To(HaveStructuredError(testErrorCode, testMessageID))
		Expect(stringMap).To(HaveStructuredError(testErrorCode, testMessageID))
		Expect(&reflected).To(HaveStructuredError(testErrorCode, testMessageID))
		Expect((*struct{ Code string })(nil)).NotTo(HaveStructuredError(testErrorCode, testMessageID))
		Expect([]byte("{")).NotTo(HaveStructuredError(testErrorCode, testMessageID))
		Expect(detail).To(HaveMessageID(testMessageID))
	})

	ginkgo.It("rejects unsupported structured error shapes without exposing parser booleans", func() {
		detail := sharederrors.NewLocalizedErrorDetailFromCode(testErrorCode)
		detailPtr := &detail
		localized := sharederrors.NewLocalizedErrorFromCode(testErrorCode, errors.New("store unavailable"))
		messageIDOnly := map[string]any{"messageID": testMessageID}

		Expect(nil).To(matchStructuredErrorExtraction(matcherRejected, newFixtureErrorCode(""), newFixtureMessageID("")))
		Expect((*sharederrors.LocalizedErrorDetail)(nil)).To(matchStructuredErrorExtraction(matcherRejected, newFixtureErrorCode(""), newFixtureMessageID("")))
		Expect((*sharederrors.LocalizedError)(nil)).To(matchStructuredLocalizedErrorExtraction(matcherRejected, newFixtureErrorCode(""), newFixtureMessageID("")))
		Expect(struct{}{}).To(matchStructuredLocalizedErrorExtraction(matcherRejected, newFixtureErrorCode(""), newFixtureMessageID("")))
		Expect(map[string]string{}).To(matchStructuredErrorExtraction(matcherRejected, newFixtureErrorCode(""), newFixtureMessageID("")))
		Expect([]string{"not", "an", "error"}).To(matchStructuredErrorExtraction(matcherRejected, newFixtureErrorCode(""), newFixtureMessageID("")))
		Expect(&detailPtr).To(matchStructuredErrorExtraction(matcherObserved, testErrorCode, testMessageID))
		Expect(&detail).To(matchStructuredErrorExtraction(matcherObserved, testErrorCode, testMessageID))
		Expect(localized).To(matchStructuredLocalizedErrorExtraction(matcherObserved, testErrorCode, testMessageID))
		Expect(*localized).To(matchStructuredErrorExtraction(matcherObserved, testErrorCode, testMessageID))
		Expect(messageIDOnly).To(matchStructuredErrorExtraction(matcherObserved, newFixtureErrorCode(""), testMessageID))
		Expect(detail).To(matchReflectDereference(matcherObserved, reflect.Struct))
		Expect(nil).To(matchReflectDereference(matcherRejected, reflect.Invalid))
	})

	ginkgo.It("matches HTTP structured errors on net/http responses while preserving readable bodies", func() {
		detail := sharederrors.NewLocalizedErrorDetailFromCode(testErrorCode)
		raw, err := json.Marshal(map[string]any{"error": detail})
		Expect(err).To(Succeed())
		response := &http.Response{
			StatusCode: http.StatusConflict,
			Body:       io.NopCloser(bytes.NewReader(raw)),
		}

		Expect(response).To(HaveHTTPStructuredError(http.StatusConflict, testErrorCode, testMessageID))

		restored, err := io.ReadAll(response.Body)
		Expect(err).To(Succeed())
		Expect(restored).To(MatchJSON(raw))

		unreadable := &http.Response{StatusCode: http.StatusConflict, Body: failingReadCloser{}}
		matcher := HaveHTTPStructuredError(http.StatusConflict, testErrorCode, testMessageID)
		_, err = matcher.Match(unreadable)
		Expect(err).To(MatchError(errMatcherBodyReadFailed))
	})

	ginkgo.It("observes HTTP matcher support across value and nil response forms", func() {
		detail := sharederrors.NewLocalizedErrorDetailFromCode(testErrorCode)
		raw, err := json.Marshal(map[string]any{"error": detail})
		Expect(err).To(Succeed())
		rec := httptest.NewRecorder()
		rec.WriteHeader(http.StatusConflict)
		_, err = rec.Write(raw)
		Expect(err).To(Succeed())
		responseValue := http.Response{
			StatusCode: http.StatusConflict,
			Body:       io.NopCloser(bytes.NewReader(raw)),
		}
		matcher := HaveHTTPStructuredError(http.StatusConflict, testErrorCode, testMessageID)

		Expect(*rec).To(HaveHTTPStructuredError(http.StatusConflict, testErrorCode, testMessageID))
		Expect(responseValue).To(HaveHTTPStructuredError(http.StatusConflict, testErrorCode, testMessageID))
		Expect(struct{}{}).To(matchMatcherAttempt(matcher, "not an HTTP response", matcherErrored))
		Expect(struct{}{}).To(matchMatcherAttempt(matcher, (*httptest.ResponseRecorder)(nil), matcherErrored))
		Expect(struct{}{}).To(matchMatcherAttempt(matcher, (*http.Response)(nil), matcherErrored))
		Expect(struct{}{}).To(matchMatcherAttempt(matcher, &httptest.ResponseRecorder{Code: http.StatusConflict}, matcherErrored))
		Expect(struct{}{}).To(matchMatcherAttempt(matcher, &http.Response{StatusCode: http.StatusConflict}, matcherErrored))
		Expect(struct{}{}).To(matchMatcherAttempt(matcher, &http.Response{StatusCode: http.StatusOK}, matcherRejected))
		Expect(struct{}{}).To(matchMatcherAttempt(matcher, &http.Response{
			StatusCode: http.StatusConflict,
			Body:       io.NopCloser(bytes.NewReader([]byte("{}"))),
		}, matcherRejected))
		Expect(struct{}{}).To(matchMatcherAttempt(matcher, &http.Response{
			StatusCode: http.StatusConflict,
			Body: io.NopCloser(bytes.NewReader([]byte(`{
				"error": {
					"code": "different",
					"messageId": "errors.different"
				}
			}`))),
		}, matcherRejected))
		Expect((*http.Response)(nil)).To(matchHTTPBodyExtraction(matcherErrored, ""))
		Expect((*httptest.ResponseRecorder)(nil)).To(matchHTTPBodyExtraction(matcherErrored, ""))
		Expect("not an HTTP response").To(matchHTTPBodyExtraction(matcherErrored, ""))
	})

})

var errMatcherBodyReadFailed = errors.New("matcher response body read failed")

type failingReadCloser struct{}

func (failingReadCloser) Read([]byte) (int, error) {
	return 0, errMatcherBodyReadFailed
}

func (failingReadCloser) Close() error {
	return nil
}

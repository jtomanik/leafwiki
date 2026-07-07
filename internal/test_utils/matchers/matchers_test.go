package matchers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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

func newFixtureErrorCode[T ~string](raw T) sharederrors.ErrorCode {
	return sharederrors.ErrorCode(raw)
}

func newFixtureMessageID[T ~string](raw T) sharederrors.MessageID {
	return sharederrors.MessageID(raw)
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
				"code":      testErrorCode.String(),
				"messageId": testMessageID.String(),
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
		Expect(err).NotTo(HaveOccurred())
		_, err = rec.Write(body)
		Expect(err).NotTo(HaveOccurred())

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
			Code:      testErrorCode.String(),
			MessageID: testMessageID.String(),
			Message:   "Page not found",
		}

		Expect(result).To(HaveMCPStructuredError(testErrorCode, testMessageID))
	})
})

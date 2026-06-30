package matchers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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

type structuredErrorData struct {
	Code      string
	MessageID string
	Message   string
	Text      string
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

func extractStructuredError(actual any) (structuredErrorData, bool) {
	if actual == nil {
		return structuredErrorData{}, false
	}
	if err, ok := actual.(error); ok {
		if localized, ok := sharederrors.AsLocalizedError(err); ok {
			return structuredErrorData{
				Code:      localized.Code.String(),
				MessageID: localized.MessageID.String(),
				Message:   localized.Message,
			}, true
		}
	}
	switch value := actual.(type) {
	case sharederrors.LocalizedErrorDetail:
		return structuredErrorData{
			Code:      value.Code.String(),
			MessageID: value.MessageID.String(),
			Message:   value.Message,
		}, true
	case *sharederrors.LocalizedErrorDetail:
		if value == nil {
			return structuredErrorData{}, false
		}
		return extractStructuredError(*value)
	case sharederrors.LocalizedError:
		return structuredErrorData{
			Code:      value.Code.String(),
			MessageID: value.MessageID.String(),
			Message:   value.Message,
		}, true
	case *sharederrors.LocalizedError:
		if value == nil {
			return structuredErrorData{}, false
		}
		return extractStructuredError(*value)
	case map[string]any:
		return extractStructuredErrorMap(value)
	case map[string]string:
		return structuredErrorData{
			Code:      value["code"],
			MessageID: firstNonEmpty(value["messageId"], value["messageID"]),
			Message:   value["message"],
			Text:      value["text"],
		}, value["code"] != "" || value["messageId"] != "" || value["messageID"] != ""
	case string:
		return extractStructuredErrorJSON([]byte(value))
	case []byte:
		return extractStructuredErrorJSON(value)
	default:
		return extractStructuredErrorReflect(actual)
	}
}

func extractStructuredErrorMap(value map[string]any) (structuredErrorData, bool) {
	if nested, ok := value["error"]; ok {
		return extractStructuredError(nested)
	}
	data := structuredErrorData{
		Code:      stringFromAny(value["code"]),
		MessageID: firstNonEmpty(stringFromAny(value["messageId"]), stringFromAny(value["messageID"])),
		Message:   stringFromAny(value["message"]),
		Text:      stringFromAny(value["text"]),
	}
	return data, data.Code != "" || data.MessageID != ""
}

func extractStructuredErrorJSON(raw []byte) (structuredErrorData, bool) {
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return structuredErrorData{}, false
	}
	return extractStructuredError(decoded)
}

func extractStructuredErrorReflect(actual any) (structuredErrorData, bool) {
	value := reflect.ValueOf(actual)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return structuredErrorData{}, false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return structuredErrorData{}, false
	}
	if nested := value.FieldByName("Error"); nested.IsValid() && nested.CanInterface() {
		if data, ok := extractStructuredError(nested.Interface()); ok {
			return data, true
		}
	}
	data := structuredErrorData{
		Code:      stringFromField(value, "Code"),
		MessageID: stringFromField(value, "MessageID"),
		Message:   stringFromField(value, "Message"),
		Text:      stringFromField(value, "Text"),
	}
	return data, data.Code != "" || data.MessageID != ""
}

func httpStatus(actual any) (int, bool, error) {
	switch response := actual.(type) {
	case *httptest.ResponseRecorder:
		if response == nil {
			return 0, false, fmt.Errorf("HTTP status matcher received a nil ResponseRecorder")
		}
		return response.Code, true, nil
	case httptest.ResponseRecorder:
		return response.Code, true, nil
	case *http.Response:
		if response == nil {
			return 0, false, fmt.Errorf("HTTP status matcher received a nil Response")
		}
		return response.StatusCode, true, nil
	case http.Response:
		return response.StatusCode, true, nil
	default:
		return 0, false, fmt.Errorf("HTTP status matcher expects *httptest.ResponseRecorder or *http.Response")
	}
}

func httpBody(actual any) (string, bool, error) {
	switch response := actual.(type) {
	case *httptest.ResponseRecorder:
		if response == nil || response.Body == nil {
			return "", false, fmt.Errorf("HTTP body matcher received a nil ResponseRecorder body")
		}
		return response.Body.String(), true, nil
	case httptest.ResponseRecorder:
		if response.Body == nil {
			return "", false, fmt.Errorf("HTTP body matcher received a nil ResponseRecorder body")
		}
		return response.Body.String(), true, nil
	case *http.Response:
		if response == nil || response.Body == nil {
			return "", false, fmt.Errorf("HTTP body matcher received a nil Response body")
		}
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			return "", false, err
		}
		response.Body = io.NopCloser(bytes.NewReader(raw))
		return string(raw), true, nil
	case http.Response:
		if response.Body == nil {
			return "", false, fmt.Errorf("HTTP body matcher received a nil Response body")
		}
		raw, err := io.ReadAll(response.Body)
		if err != nil {
			return "", false, err
		}
		return string(raw), true, nil
	default:
		return "", false, fmt.Errorf("HTTP body matcher expects *httptest.ResponseRecorder or *http.Response")
	}
}

type fieldErrorData struct {
	Field     string
	Code      string
	MessageID string
}

func extractFieldErrors(actual any) ([]fieldErrorData, bool) {
	switch value := actual.(type) {
	case *sharederrors.ValidationErrors:
		if value == nil {
			return nil, false
		}
		return fieldErrorsFromPointers(value.Errors), true
	case sharederrors.ValidationErrors:
		return fieldErrorsFromPointers(value.Errors), true
	case []*sharederrors.FieldError:
		return fieldErrorsFromPointers(value), true
	case []sharederrors.FieldError:
		out := make([]fieldErrorData, 0, len(value))
		for _, field := range value {
			out = append(out, fieldErrorFromValue(field))
		}
		return out, true
	default:
		return extractFieldErrorsReflect(actual)
	}
}

func fieldErrorsFromPointers(values []*sharederrors.FieldError) []fieldErrorData {
	out := make([]fieldErrorData, 0, len(values))
	for _, field := range values {
		if field == nil {
			continue
		}
		out = append(out, fieldErrorFromValue(*field))
	}
	return out
}

func fieldErrorFromValue(value sharederrors.FieldError) fieldErrorData {
	return fieldErrorData{
		Field:     value.Field,
		Code:      value.Code.String(),
		MessageID: value.MessageID.String(),
	}
}

func extractFieldErrorsReflect(actual any) ([]fieldErrorData, bool) {
	value := reflect.ValueOf(actual)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, false
		}
		value = value.Elem()
	}
	if value.Kind() == reflect.Struct {
		if fields := value.FieldByName("Errors"); fields.IsValid() && fields.CanInterface() {
			return extractFieldErrors(fields.Interface())
		}
	}
	if value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
		return nil, false
	}
	out := make([]fieldErrorData, 0, value.Len())
	for i := 0; i < value.Len(); i++ {
		item := value.Index(i)
		if item.Kind() == reflect.Pointer {
			if item.IsNil() {
				continue
			}
			item = item.Elem()
		}
		if item.Kind() != reflect.Struct {
			return nil, false
		}
		out = append(out, fieldErrorData{
			Field:     stringFromField(item, "Field"),
			Code:      stringFromField(item, "Code"),
			MessageID: stringFromField(item, "MessageID"),
		})
	}
	return out, true
}

func extractValidationIssueCodes(actual any) ([]string, bool) {
	switch value := actual.(type) {
	case wikivalidation.Result:
		return validationIssueCodes(value.Issues), true
	case *wikivalidation.Result:
		if value == nil {
			return nil, false
		}
		return validationIssueCodes(value.Issues), true
	case []wikivalidation.Issue:
		return validationIssueCodes(value), true
	default:
		return extractValidationIssueCodesReflect(actual)
	}
}

func validationIssueCodes(issues []wikivalidation.Issue) []string {
	codes := make([]string, 0, len(issues))
	for _, issue := range issues {
		codes = append(codes, issue.Code.String())
	}
	return codes
}

func extractValidationIssueCodesReflect(actual any) ([]string, bool) {
	value := reflect.ValueOf(actual)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, false
		}
		value = value.Elem()
	}
	if value.Kind() == reflect.Struct {
		if issues := value.FieldByName("Issues"); issues.IsValid() && issues.CanInterface() {
			return extractValidationIssueCodes(issues.Interface())
		}
	}
	if value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
		return nil, false
	}
	codes := make([]string, 0, value.Len())
	for i := 0; i < value.Len(); i++ {
		item := value.Index(i)
		if item.Kind() == reflect.Pointer {
			if item.IsNil() {
				continue
			}
			item = item.Elem()
		}
		if item.Kind() != reflect.Struct {
			return nil, false
		}
		code := stringFromField(item, "Code")
		if code != "" {
			codes = append(codes, code)
		}
	}
	return codes, true
}

func stringFromField(value reflect.Value, name string) string {
	field := value.FieldByName(name)
	if !field.IsValid() || !field.CanInterface() {
		return ""
	}
	return stringFromAny(field.Interface())
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	default:
		if reflect.TypeOf(value).Kind() == reflect.String {
			return reflect.ValueOf(value).String()
		}
		return fmt.Sprint(value)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

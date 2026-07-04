package matchers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"

	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

type structuredErrorData struct {
	Code      string
	MessageID string
	Message   string
	Text      string
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

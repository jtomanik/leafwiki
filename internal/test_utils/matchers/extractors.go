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
	if data, ok := extractStructuredErrorFromError(actual); ok {
		return data, true
	}
	return extractStructuredErrorValue(actual)
}

func extractStructuredErrorFromError(actual any) (structuredErrorData, bool) {
	if err, ok := actual.(error); ok {
		if localized, ok := sharederrors.AsLocalizedError(err); ok {
			return structuredErrorData{
				Code:      localized.Code.String(),
				MessageID: localized.MessageID.String(),
				Message:   localized.Message,
			}, true
		}
	}
	return structuredErrorData{}, false
}

func extractStructuredErrorValue(actual any) (structuredErrorData, bool) {
	if data, ok := extractStructuredErrorDetail(actual); ok {
		return data, true
	}
	if data, ok := extractStructuredLocalizedError(actual); ok {
		return data, true
	}
	if data, ok := extractStructuredErrorMapValue(actual); ok {
		return data, true
	}
	if data, ok := extractStructuredErrorRawValue(actual); ok {
		return data, true
	}
	return extractStructuredErrorReflect(actual)
}

func extractStructuredErrorDetail(actual any) (structuredErrorData, bool) {
	switch value := actual.(type) {
	case sharederrors.LocalizedErrorDetail:
		return structuredErrorDataFromLocalizedDetail(value), true
	case *sharederrors.LocalizedErrorDetail:
		if value == nil {
			return structuredErrorData{}, false
		}
		return structuredErrorDataFromLocalizedDetail(*value), true
	default:
		return structuredErrorData{}, false
	}
}

func extractStructuredLocalizedError(actual any) (structuredErrorData, bool) {
	switch value := actual.(type) {
	case sharederrors.LocalizedError:
		return structuredErrorDataFromLocalizedError(value), true
	case *sharederrors.LocalizedError:
		if value == nil {
			return structuredErrorData{}, false
		}
		return structuredErrorDataFromLocalizedError(*value), true
	default:
		return structuredErrorData{}, false
	}
}

func extractStructuredErrorMapValue(actual any) (structuredErrorData, bool) {
	switch value := actual.(type) {
	case map[string]any:
		return extractStructuredErrorMap(value)
	case map[string]string:
		return extractStructuredErrorStringMap(value)
	default:
		return structuredErrorData{}, false
	}
}

func extractStructuredErrorRawValue(actual any) (structuredErrorData, bool) {
	switch value := actual.(type) {
	case string:
		return extractStructuredErrorJSON([]byte(value))
	case []byte:
		return extractStructuredErrorJSON(value)
	default:
		return structuredErrorData{}, false
	}
}

func structuredErrorDataFromLocalizedDetail(value sharederrors.LocalizedErrorDetail) structuredErrorData {
	return structuredErrorData{
		Code:      value.Code.String(),
		MessageID: value.MessageID.String(),
		Message:   value.Message,
	}
}

func structuredErrorDataFromLocalizedError(value sharederrors.LocalizedError) structuredErrorData {
	return structuredErrorData{
		Code:      value.Code.String(),
		MessageID: value.MessageID.String(),
		Message:   value.Message,
	}
}

func extractStructuredErrorStringMap(value map[string]string) (structuredErrorData, bool) {
	data := structuredErrorData{
		Code:      value["code"],
		MessageID: firstNonEmpty(value["messageId"], value["messageID"]),
		Message:   value["message"],
		Text:      value["text"],
	}
	return data, data.Code != "" || data.MessageID != ""
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
	value, ok := dereferenceReflectValue(reflect.ValueOf(actual))
	if !ok {
		return structuredErrorData{}, false
	}
	if value.Kind() != reflect.Struct {
		return structuredErrorData{}, false
	}
	if data, ok := extractNestedStructuredError(value); ok {
		return data, true
	}
	return structuredErrorDataFromReflectValue(value)
}

func extractNestedStructuredError(value reflect.Value) (structuredErrorData, bool) {
	nested, ok := reflectStructField(value, "Error")
	if !ok {
		return structuredErrorData{}, false
	}
	return extractStructuredError(nested.Interface())
}

func structuredErrorDataFromReflectValue(value reflect.Value) (structuredErrorData, bool) {
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
		return responseRecorderStatus(response)
	case httptest.ResponseRecorder:
		return responseRecorderStatus(&response)
	case *http.Response:
		return responseStatus(response)
	case http.Response:
		return responseStatus(&response)
	default:
		return 0, false, fmt.Errorf("HTTP status matcher expects *httptest.ResponseRecorder or *http.Response")
	}
}

func responseRecorderStatus(response *httptest.ResponseRecorder) (int, bool, error) {
	if response == nil {
		return 0, false, fmt.Errorf("HTTP status matcher received a nil ResponseRecorder")
	}
	return response.Code, true, nil
}

func responseStatus(response *http.Response) (int, bool, error) {
	if response == nil {
		return 0, false, fmt.Errorf("HTTP status matcher received a nil Response")
	}
	return response.StatusCode, true, nil
}

func httpBody(actual any) (string, bool, error) {
	switch response := actual.(type) {
	case *httptest.ResponseRecorder:
		return responseRecorderBody(response)
	case httptest.ResponseRecorder:
		return responseRecorderBody(&response)
	case *http.Response:
		return responseBody(response, true)
	case http.Response:
		return responseBody(&response, false)
	default:
		return "", false, fmt.Errorf("HTTP body matcher expects *httptest.ResponseRecorder or *http.Response")
	}
}

func responseRecorderBody(response *httptest.ResponseRecorder) (string, bool, error) {
	if response == nil || response.Body == nil {
		return "", false, fmt.Errorf("HTTP body matcher received a nil ResponseRecorder body")
	}
	return response.Body.String(), true, nil
}

func responseBody(response *http.Response, restoreBody bool) (string, bool, error) {
	if response == nil || response.Body == nil {
		return "", false, fmt.Errorf("HTTP body matcher received a nil Response body")
	}
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		return "", false, err
	}
	if restoreBody {
		response.Body = io.NopCloser(bytes.NewReader(raw))
	}
	return string(raw), true, nil
}

type fieldErrorData struct {
	Field     string
	Code      string
	MessageID string
}

func extractFieldErrors(actual any) ([]fieldErrorData, bool) {
	if fields, ok := extractValidationErrorFields(actual); ok {
		return fields, true
	}
	if fields, ok := extractFieldErrorSlice(actual); ok {
		return fields, true
	}
	return extractFieldErrorsReflect(actual)
}

func extractValidationErrorFields(actual any) ([]fieldErrorData, bool) {
	switch value := actual.(type) {
	case *sharederrors.ValidationErrors:
		if value == nil {
			return nil, false
		}
		return fieldErrorsFromPointers(value.Errors), true
	case sharederrors.ValidationErrors:
		return fieldErrorsFromPointers(value.Errors), true
	default:
		return nil, false
	}
}

func extractFieldErrorSlice(actual any) ([]fieldErrorData, bool) {
	switch value := actual.(type) {
	case []*sharederrors.FieldError:
		return fieldErrorsFromPointers(value), true
	case []sharederrors.FieldError:
		return fieldErrorsFromValues(value), true
	default:
		return nil, false
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

func fieldErrorsFromValues(values []sharederrors.FieldError) []fieldErrorData {
	out := make([]fieldErrorData, 0, len(values))
	for _, field := range values {
		out = append(out, fieldErrorFromValue(field))
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
	value, ok := dereferenceReflectValue(reflect.ValueOf(actual))
	if !ok {
		return nil, false
	}
	if fields, ok := reflectStructField(value, "Errors"); ok {
		return extractFieldErrors(fields.Interface())
	}
	if value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
		return nil, false
	}
	return fieldErrorsFromReflectSequence(value)
}

func fieldErrorsFromReflectSequence(value reflect.Value) ([]fieldErrorData, bool) {
	out := make([]fieldErrorData, 0, value.Len())
	for i := 0; i < value.Len(); i++ {
		item, ok := dereferenceReflectValue(value.Index(i))
		if !ok {
			continue
		}
		field, ok := fieldErrorFromReflectValue(item)
		if !ok {
			return nil, false
		}
		out = append(out, field)
	}
	return out, true
}

func fieldErrorFromReflectValue(value reflect.Value) (fieldErrorData, bool) {
	if value.Kind() != reflect.Struct {
		return fieldErrorData{}, false
	}
	return fieldErrorData{
		Field:     stringFromField(value, "Field"),
		Code:      stringFromField(value, "Code"),
		MessageID: stringFromField(value, "MessageID"),
	}, true
}

func extractValidationIssueCodes(actual any) ([]string, bool) {
	if codes, ok := extractValidationResultIssueCodes(actual); ok {
		return codes, true
	}
	if codes, ok := extractValidationIssueSliceCodes(actual); ok {
		return codes, true
	}
	return extractValidationIssueCodesReflect(actual)
}

func extractValidationResultIssueCodes(actual any) ([]string, bool) {
	switch value := actual.(type) {
	case wikivalidation.Result:
		return validationIssueCodes(value.Issues), true
	case *wikivalidation.Result:
		if value == nil {
			return nil, false
		}
		return validationIssueCodes(value.Issues), true
	default:
		return nil, false
	}
}

func extractValidationIssueSliceCodes(actual any) ([]string, bool) {
	switch value := actual.(type) {
	case []wikivalidation.Issue:
		return validationIssueCodes(value), true
	default:
		return nil, false
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
	value, ok := dereferenceReflectValue(reflect.ValueOf(actual))
	if !ok {
		return nil, false
	}
	if codes, ok := extractValidationIssueCodesFromReflectStruct(value); ok {
		return codes, true
	}
	return extractValidationIssueCodesFromReflectSequence(value)
}

func extractValidationIssueCodesFromReflectStruct(value reflect.Value) ([]string, bool) {
	issues, ok := reflectStructField(value, "Issues")
	if !ok {
		return nil, false
	}
	return extractValidationIssueCodes(issues.Interface())
}

func extractValidationIssueCodesFromReflectSequence(value reflect.Value) ([]string, bool) {
	if value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
		return nil, false
	}
	return validationIssueCodesFromReflectSequence(value)
}

func validationIssueCodesFromReflectSequence(value reflect.Value) ([]string, bool) {
	codes := make([]string, 0, value.Len())
	for i := 0; i < value.Len(); i++ {
		item, ok := dereferenceReflectValue(value.Index(i))
		if !ok {
			continue
		}
		if item.Kind() != reflect.Struct {
			return nil, false
		}
		codes = appendReflectIssueCode(codes, item)
	}
	return codes, true
}

func appendReflectIssueCode(codes []string, item reflect.Value) []string {
	code := stringFromField(item, "Code")
	if code == "" {
		return codes
	}
	return append(codes, code)
}

func dereferenceReflectValue(value reflect.Value) (reflect.Value, bool) {
	if !value.IsValid() {
		return reflect.Value{}, false
	}
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}, false
		}
		value = value.Elem()
	}
	return value, true
}

func reflectStructField(value reflect.Value, name string) (reflect.Value, bool) {
	if value.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	field := value.FieldByName(name)
	return field, field.IsValid() && field.CanInterface()
}

func stringFromField(value reflect.Value, name string) string {
	field := value.FieldByName(name)
	if !field.IsValid() || !field.CanInterface() {
		return ""
	}
	return stringFromAny(field.Interface())
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

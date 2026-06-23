package errors

type FieldErrorCode string

func (code FieldErrorCode) String() string {
	return string(code)
}

const (
	FieldValidationErrorCode      FieldErrorCode = "field_validation_error"
	FieldValidationErrorMessageID MessageID      = "validation.field.validation_error"
)

type FieldError struct {
	Field     string         `json:"field"`
	Code      FieldErrorCode `json:"code,omitempty"`
	MessageID MessageID      `json:"messageId,omitempty"`
	Message   string         `json:"message"`
}

func NewFieldError(field, message string) *FieldError {
	return NewFieldErrorWithCode(
		field,
		FieldValidationErrorCode,
		FieldValidationErrorMessageID,
		message,
	)
}

func NewFieldErrorWithCode(field string, code FieldErrorCode, messageID MessageID, message string) *FieldError {
	return &FieldError{
		Field:     field,
		Code:      code,
		MessageID: messageID,
		Message:   message,
	}
}

type ValidationErrors struct {
	Errors []*FieldError `json:"fields"`
}

func NewValidationErrors() *ValidationErrors {
	return &ValidationErrors{Errors: []*FieldError{}}
}

func (v *ValidationErrors) Add(field, message string) {
	v.Errors = append(v.Errors, NewFieldError(field, message))
}

func (v *ValidationErrors) AddWithCode(field string, code FieldErrorCode, messageID MessageID, message string) {
	v.Errors = append(v.Errors, NewFieldErrorWithCode(field, code, messageID, message))
}

func (v *ValidationErrors) Error() string {
	return "validation error"
}

func (v *ValidationErrors) HasErrors() bool {
	return len(v.Errors) > 0
}

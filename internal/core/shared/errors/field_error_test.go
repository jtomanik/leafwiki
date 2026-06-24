package errors

import (
	"encoding/json"
	"testing"
)

func TestValidationErrorsAddWithCodeSerializesStableFieldContract(t *testing.T) {
	t.Parallel()

	validation := NewValidationErrors()
	validation.AddWithCode(
		"slug",
		"auth_email_invalid",
		"validation.auth.email_invalid",
	)

	encoded, err := json.Marshal(validation)
	if err != nil {
		t.Fatalf("marshal validation errors: %v", err)
	}

	want := `{"fields":[{"field":"slug","code":"auth_email_invalid","messageId":"validation.auth.email_invalid","message":"Email is not valid"}]}`
	if string(encoded) != want {
		t.Fatalf("json = %s, want %s", encoded, want)
	}
}

func TestValidationErrorsLegacyAddKeepsMessageAndProvidesDefaultCode(t *testing.T) {
	t.Parallel()

	validation := NewValidationErrors()
	validation.Add("siteName", "site name is required")

	if len(validation.Errors) != 1 {
		t.Fatalf("fields = %d, want 1", len(validation.Errors))
	}
	field := validation.Errors[0]
	if field.Code != FieldValidationErrorCode {
		t.Fatalf("Code = %q, want field_validation_error", field.Code)
	}
	if field.MessageID != FieldValidationErrorMessageID {
		t.Fatalf("MessageID = %q, want validation.field.validation_error", field.MessageID)
	}
	if field.Field != "siteName" || field.Message != "site name is required" {
		t.Fatalf("field error = %#v", field)
	}
}

func TestValidationErrorsAddWithCodeRendersFromCatalog(t *testing.T) {
	t.Parallel()

	validation := NewValidationErrors()
	validation.AddWithCode(
		"email",
		"auth_email_invalid",
		"validation.auth.email_invalid",
	)

	field := validation.Errors[0]
	if field.Message != "Email is not valid" {
		t.Fatalf("Message = %q, want catalog-rendered validation message", field.Message)
	}
	if field.Code != "auth_email_invalid" {
		t.Fatalf("Code = %q, want stable field code", field.Code)
	}
	if field.MessageID != "validation.auth.email_invalid" {
		t.Fatalf("MessageID = %q, want stable message ID", field.MessageID)
	}
}

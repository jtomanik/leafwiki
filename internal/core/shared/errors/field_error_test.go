package errors

import (
	"encoding/json"
	"testing"
)

const (
	testPageSlugRequiredCode      FieldErrorCode = "page_slug_required"
	testPageSlugRequiredMessageID MessageID      = "validation.page.slug_required"
)

func TestValidationErrorsAddWithCodeSerializesStableFieldContract(t *testing.T) {
	t.Parallel()

	validation := NewValidationErrors()
	validation.AddWithCode(
		"slug",
		testPageSlugRequiredCode,
		testPageSlugRequiredMessageID,
		"Slug is required",
	)

	encoded, err := json.Marshal(validation)
	if err != nil {
		t.Fatalf("marshal validation errors: %v", err)
	}

	want := `{"fields":[{"field":"slug","code":"page_slug_required","messageId":"validation.page.slug_required","message":"Slug is required"}]}`
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

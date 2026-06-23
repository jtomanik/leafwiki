package errors

import (
	"encoding/json"
	"errors"
	"testing"
)

const (
	testPageVersionConflictCode      ErrorCode = "page_version_conflict"
	testPageVersionConflictMessageID MessageID = "errors.page.version_conflict"
	testAuthInvalidCredentialsCode   ErrorCode = "auth_invalid_credentials"
	testAuthInvalidCredentialsMsgID  MessageID = "errors.auth.invalid_credentials"
)

func TestNewDefinedLocalizedErrorExposesTypedCodeAndMessageID(t *testing.T) {
	t.Parallel()

	cause := errors.New("storage failed")
	definition := ErrorDefinition{
		Code:      testPageVersionConflictCode,
		MessageID: testPageVersionConflictMessageID,
		Message:   "Page was changed by another request",
		Template:  "page was changed by another request",
	}

	err := NewDefinedLocalizedError(definition, cause, "page-1")

	if err.Code != testPageVersionConflictCode {
		t.Fatalf("Code = %q, want typed page_version_conflict", err.Code)
	}
	if err.MessageID != testPageVersionConflictMessageID {
		t.Fatalf("MessageID = %q, want errors.page.version_conflict", err.MessageID)
	}
	if err.Message != "Page was changed by another request" || err.Template != "page was changed by another request" {
		t.Fatalf("localized message/template = %q/%q", err.Message, err.Template)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("localized error does not unwrap cause")
	}
	if len(err.Args) != 1 || err.Args[0] != "page-1" {
		t.Fatalf("Args = %#v, want [page-1]", err.Args)
	}
}

func TestNewLocalizedErrorKeepsLegacyConstructorButAddsDefaultMessageID(t *testing.T) {
	t.Parallel()

	err := NewLocalizedError(testAuthInvalidCredentialsCode, "Invalid credentials", "invalid credentials", nil)

	if err.Code != testAuthInvalidCredentialsCode {
		t.Fatalf("Code = %q, want typed auth_invalid_credentials", err.Code)
	}
	if err.MessageID != testAuthInvalidCredentialsMsgID {
		t.Fatalf("MessageID = %q, want errors.auth.invalid_credentials", err.MessageID)
	}
}

func TestLocalizedErrorDetailSerializesMessageIDWithCompatibilityFields(t *testing.T) {
	t.Parallel()

	detail := NewLocalizedErrorDetail(
		testPageVersionConflictCode,
		"Page was changed by another request",
		"page was changed by another request",
		"page-1",
	)

	encoded, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("marshal detail: %v", err)
	}

	want := `{"code":"page_version_conflict","messageId":"errors.page.version_conflict","message":"Page was changed by another request","template":"page was changed by another request","args":["page-1"]}`
	if string(encoded) != want {
		t.Fatalf("json = %s, want %s", encoded, want)
	}
}

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

func TestNewLocalizedErrorFromCodeRendersCatalogMessage(t *testing.T) {
	t.Parallel()

	cause := errors.New("storage failed")

	err := NewLocalizedErrorFromCode(testPageVersionConflictCode, cause, "docs.md", "README.md")

	if err.Code != testPageVersionConflictCode {
		t.Fatalf("Code = %q, want %q", err.Code, testPageVersionConflictCode)
	}
	if err.MessageID != testPageVersionConflictMessageID {
		t.Fatalf("MessageID = %q, want %q", err.MessageID, testPageVersionConflictMessageID)
	}
	if err.Message != "Page docs.md was changed by another request before README.md could be saved." {
		t.Fatalf("Message = %q, want catalog-rendered conflict message", err.Message)
	}
	if err.Template != err.Message {
		t.Fatalf("Template = %q, want rendered catalog message", err.Template)
	}
	if len(err.Args) != 2 || err.Args[0] != "docs.md" || err.Args[1] != "README.md" {
		t.Fatalf("Args = %#v, want preserved args", err.Args)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("localized error does not unwrap cause")
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

func TestLocalizedErrorDetailRendersMessageFromCatalog(t *testing.T) {
	t.Parallel()

	detail := NewLocalizedErrorDetail(
		testAuthInvalidCredentialsCode,
		"legacy fallback",
		"legacy fallback",
	)

	if detail.Message != "Invalid credentials" {
		t.Fatalf("Message = %q, want catalog-rendered Invalid credentials", detail.Message)
	}
	if detail.Template != "legacy fallback" {
		t.Fatalf("Template = %q, want compatibility template", detail.Template)
	}
	if detail.MessageID != testAuthInvalidCredentialsMsgID {
		t.Fatalf("MessageID = %q, want %q", detail.MessageID, testAuthInvalidCredentialsMsgID)
	}
}

func TestLocalizedErrorDetailUsesArgNBridgeAndPreservesArgs(t *testing.T) {
	t.Parallel()

	err := NewDefinedLocalizedError(ErrorDefinition{
		Code:      testPageVersionConflictCode,
		MessageID: testPageVersionConflictMessageID,
		Message:   "legacy fallback",
		Template:  "page %s could not be saved before %s",
	}, nil, "docs.md", "README.md")

	detail := LocalizedErrorDetailFromError(err)

	if detail.Message != "Page docs.md was changed by another request before README.md could be saved." {
		t.Fatalf("Message = %q, want catalog-rendered page conflict", detail.Message)
	}
	if len(detail.Args) != 2 || detail.Args[0] != "docs.md" || detail.Args[1] != "README.md" {
		t.Fatalf("Args = %#v, want compatibility args preserved", detail.Args)
	}
	if detail.Template != "page %s could not be saved before %s" {
		t.Fatalf("Template = %q, want compatibility template preserved", detail.Template)
	}
}

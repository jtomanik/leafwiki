package workspaceid

import (
	"errors"
	"testing"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

func TestParseWorkspaceIDReturnsSemanticID(t *testing.T) {
	t.Parallel()

	id, err := ParseWorkspaceID("docs-home")
	if err != nil {
		t.Fatalf("ParseWorkspaceID: %v", err)
	}
	if id != WorkspaceID("docs-home") {
		t.Fatalf("WorkspaceID = %q, want docs-home", id)
	}
	if id.String() != "docs-home" {
		t.Fatalf("WorkspaceID.String = %q, want docs-home", id.String())
	}
}

func TestParseWorkspaceIDRejectsInvalidInputWithTypedCode(t *testing.T) {
	t.Parallel()

	_, err := ParseWorkspaceID(" Docs ")
	if err == nil {
		t.Fatalf("ParseWorkspaceID unexpectedly accepted whitespace")
	}

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %T %v, want *ValidationError", err, err)
	}
	if validationErr.Code != ErrCodeWorkspaceIDWhitespace {
		t.Fatalf("Code = %q, want %q", validationErr.Code, ErrCodeWorkspaceIDWhitespace)
	}
	if got := WorkspaceIDErrorCode(err); got != sharederrors.ErrorCode("workspace_id_whitespace") {
		t.Fatalf("WorkspaceIDErrorCode = %q, want workspace_id_whitespace", got)
	}
}

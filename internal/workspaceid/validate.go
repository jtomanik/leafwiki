package workspaceid

import (
	"database/sql/driver"
	stderrors "errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

var workspaceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

type WorkspaceID string

func (id WorkspaceID) String() string {
	return string(id)
}

func (id WorkspaceID) HTTPHeaderValue() string {
	return string(id)
}

func (id WorkspaceID) URLPathSegment() string {
	return url.PathEscape(string(id))
}

func (id WorkspaceID) StorageKey() string {
	return string(id)
}

func (id WorkspaceID) Validate() error {
	raw := string(id)
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return &ValidationError{Code: ErrCodeWorkspaceIDRequired, Message: "workspace ID is required"}
	}
	if trimmed != raw {
		return &ValidationError{
			Code:    ErrCodeWorkspaceIDWhitespace,
			Message: fmt.Sprintf("workspace ID %q must not contain leading or trailing whitespace", raw),
		}
	}
	if !workspaceIDPattern.MatchString(raw) {
		return &ValidationError{
			Code:    ErrCodeWorkspaceIDInvalid,
			Message: fmt.Sprintf("workspace ID %q must be URL-safe lowercase letters, numbers, and dashes", raw),
		}
	}
	return nil
}

func (id WorkspaceID) Value() (driver.Value, error) {
	if err := id.Validate(); err != nil {
		return nil, err
	}
	return string(id), nil
}

func (id *WorkspaceID) Scan(value any) error {
	var raw string
	switch v := value.(type) {
	case string:
		raw = v
	case []byte:
		raw = string(v)
	case nil:
		raw = ""
	default:
		return fmt.Errorf("workspace ID scan source %T is not supported", value)
	}
	parsed, err := ParseWorkspaceID(raw)
	if err != nil {
		return err
	}
	*id = parsed
	return nil
}

const (
	ErrCodeWorkspaceIDRequired   sharederrors.ErrorCode = "workspace_id_required"
	ErrCodeWorkspaceIDWhitespace sharederrors.ErrorCode = "workspace_id_whitespace"
	ErrCodeWorkspaceIDInvalid    sharederrors.ErrorCode = "workspace_id_invalid"
)

type ValidationError struct {
	Code    sharederrors.ErrorCode
	Message string
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func ParseWorkspaceID(id string) (WorkspaceID, error) {
	workspaceID := WorkspaceID(id)
	if err := workspaceID.Validate(); err != nil {
		return "", err
	}
	return workspaceID, nil
}

func ValidateWorkspaceID(id string) (WorkspaceID, error) {
	return ParseWorkspaceID(id)
}

func WorkspaceIDErrorCode(err error) sharederrors.ErrorCode {
	var validationErr *ValidationError
	if !stderrors.As(err, &validationErr) || validationErr == nil {
		return ""
	}
	return validationErr.Code
}

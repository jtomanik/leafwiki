package workspaceid

import (
	"fmt"
	"regexp"
	"strings"
)

var workspaceIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func ValidateWorkspaceID(id string) error {
	trimmed := strings.TrimSpace(id)
	if trimmed == "" {
		return fmt.Errorf("workspace ID is required")
	}
	if trimmed != id {
		return fmt.Errorf("workspace ID %q must not contain leading or trailing whitespace", id)
	}
	if !workspaceIDPattern.MatchString(id) {
		return fmt.Errorf("workspace ID %q must be URL-safe lowercase letters, numbers, and dashes", id)
	}
	return nil
}

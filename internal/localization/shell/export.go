package shell

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/perber/wiki/internal/localization"
)

var (
	ErrRunMessageRenderedEmpty  = errors.New("rendered empty")
	ErrGeneratedRunMessagesFile = errors.New("is stale; regenerate from localization catalog")
)

func GenerateRunMessages() (string, error) {
	rendered := map[string]string{}
	messages := []struct {
		name string
		id   string
	}{
		{"LEAFWIKI_RUN_MSG_USAGE", localization.MessageIDShellRunUsage},
		{"LEAFWIKI_RUN_MSG_HELP_BODY", localization.MessageIDShellRunHelpBody},
		{"LEAFWIKI_RUN_MSG_ERROR_PREFIX", localization.MessageIDShellRunErrorPrefix},
		{"LEAFWIKI_RUN_MSG_DRY_RUN_MCP_CONFIG", localization.MessageIDShellRunDryRunMCPConfig},
		{"LEAFWIKI_RUN_MSG_DRY_RUN_MCP_NATIVE", localization.MessageIDShellRunDryRunMCPNative},
		{"LEAFWIKI_RUN_MSG_DRY_RUN_STDIO_ATTACH", localization.MessageIDShellRunDryRunSTDIOAttach},
		{"LEAFWIKI_RUN_MSG_DRY_RUN_AGENT_HOOK", localization.MessageIDShellRunDryRunAgentHook},
		{"LEAFWIKI_RUN_MSG_DRY_RUN_HTTP_CONFIG", localization.MessageIDShellRunDryRunHTTPConfig},
		{"LEAFWIKI_RUN_MSG_DRY_RUN_HTTP_URL", localization.MessageIDShellRunDryRunHTTPURL},
		{"LEAFWIKI_RUN_MSG_ERROR_AGENT_HOOK_REQUIRES_PROVIDER", localization.MessageIDShellRunErrorAgentHookRequiresProvider},
		{"LEAFWIKI_RUN_MSG_ERROR_ARGUMENT_CONTAINS_SPACE", localization.MessageIDShellRunErrorArgumentContainsSpace},
		{"LEAFWIKI_RUN_MSG_ERROR_CONFIG_CANNOT_COMBINE", localization.MessageIDShellRunErrorConfigCannotCombine},
		{"LEAFWIKI_RUN_MSG_ERROR_CONFIG_REQUIRES_PATH", localization.MessageIDShellRunErrorConfigRequiresPath},
		{"LEAFWIKI_RUN_MSG_ERROR_DISABLE_AUTH_API_KEY_CONFLICT", localization.MessageIDShellRunErrorDisableAuthAPIKeyConflict},
		{"LEAFWIKI_RUN_MSG_ERROR_EXECUTABLE_NOT_FOUND", localization.MessageIDShellRunErrorExecutableNotFound},
		{"LEAFWIKI_RUN_MSG_ERROR_EXECUTABLE_NOT_FOUND_ON_PATH", localization.MessageIDShellRunErrorExecutableNotFoundOnPath},
		{"LEAFWIKI_RUN_MSG_ERROR_LEAFWIKI_BIN_REQUIRES_PATH", localization.MessageIDShellRunErrorLeafWikiBinRequiresPath},
		{"LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_ARGUMENT", localization.MessageIDShellRunErrorOptionRequiresArgument},
		{"LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_DURATION", localization.MessageIDShellRunErrorOptionRequiresDuration},
		{"LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_PASSWORD", localization.MessageIDShellRunErrorOptionRequiresPassword},
		{"LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_PATH", localization.MessageIDShellRunErrorOptionRequiresPath},
		{"LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_SECRET", localization.MessageIDShellRunErrorOptionRequiresSecret},
		{"LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_VALUE", localization.MessageIDShellRunErrorOptionRequiresValue},
		{"LEAFWIKI_RUN_MSG_ERROR_RUN_MODE_REQUIRED", localization.MessageIDShellRunErrorRunModeRequired},
		{"LEAFWIKI_RUN_MSG_ERROR_UNKNOWN_OPTION", localization.MessageIDShellRunErrorUnknownOption},
	}
	for _, message := range messages {
		result := localization.English.Render(message.id, "")
		if result.Message == "" {
			return "", fmt.Errorf("%s %w", message.id, ErrRunMessageRenderedEmpty)
		}
		rendered[message.name] = result.Message
	}

	var out strings.Builder
	out.WriteString("#!/usr/bin/env bash\n")
	out.WriteString("# Generated from internal/localization/locales/active.en.toml.\n\n")
	for _, message := range messages {
		out.WriteString(message.name)
		out.WriteString("='")
		out.WriteString(shellSingleQuote(rendered[message.name]))
		out.WriteString("'\n")
	}
	return out.String(), nil
}

func ValidateGeneratedRunMessages(path string) error {
	want, err := GenerateRunMessages()
	if err != nil {
		return err
	}
	got, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if string(got) != want {
		return fmt.Errorf("%s %w", path, ErrGeneratedRunMessagesFile)
	}
	return nil
}

func shellSingleQuote(value string) string {
	return strings.ReplaceAll(value, "'", "'\"'\"'")
}

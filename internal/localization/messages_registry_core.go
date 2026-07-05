package localization

import "github.com/nicksnyder/go-i18n/v2/i18n"

var registryMessagesCore = []*i18n.Message{
	{
		ID:          MessageIDCLIErrorByteSizeMustBePositive,
		Description: "Printed when a byte-size CLI setting is zero or negative.",
		Other:       "Byte size value must be greater than zero",
	},
	{
		ID:          MessageIDCLIErrorByteSizeTooLarge,
		Description: "Printed when a byte-size CLI setting is too large.",
		Other:       "Byte size value is too large",
	},
	{
		ID:          MessageIDCLIErrorInvalidAgentHookArguments,
		Description: "Printed when agent-hook CLI arguments are invalid.",
		Other:       "Invalid agent hook arguments",
	},
	{
		ID:          MessageIDCLIErrorInvalidByteSizeValue,
		Description: "Printed when a byte-size CLI setting cannot be parsed.",
		Other:       "Invalid byte size value",
	},
	{
		ID:          MessageIDCLIErrorInvalidConfigArguments,
		Description: "Printed when YAML config mode is combined with incompatible CLI arguments.",
		Other:       "Invalid config arguments",
	},
	{
		ID:          MessageIDCLIErrorInvalidConfigFile,
		Description: "Printed when the configured YAML file cannot be used.",
		Other:       "Invalid config file",
	},
	{
		ID:          MessageIDCLIErrorInvalidEnvironment,
		Description: "Printed when LeafWiki startup rejects the process environment.",
		Other:       "Invalid environment",
	},
	{
		ID:          MessageIDCLIErrorInvalidEnvironmentVariableValue,
		Description: "Printed when a LeafWiki environment variable has an invalid value.",
		Other:       "Invalid environment variable value",
	},
	{
		ID:          MessageIDCLIErrorInvalidHTTPRemoteUserConfig,
		Description: "Printed when HTTP remote-user authentication config is invalid.",
		Other:       "Invalid HTTP remote user configuration",
	},
	{
		ID:          MessageIDCLIErrorInvalidLoggingConfig,
		Description: "Printed when logging config is invalid.",
		Other:       "Invalid logging configuration",
	},
	{
		ID:          MessageIDCLIErrorInvalidMarkdownLinkRootPrefix,
		Description: "Printed when markdown link root prefix config is invalid.",
		Other:       "Invalid markdown link root prefix",
	},
	{
		ID:          MessageIDCLIErrorInvalidMCPConfig,
		Description: "Printed when MCP config is invalid.",
		Other:       "Invalid MCP configuration",
	},
	{
		ID:          MessageIDCLIErrorInvalidNativeSTDIOConfig,
		Description: "Printed when native STDIO config is invalid.",
		Other:       "Invalid native STDIO configuration",
	},
	{
		ID:          MessageIDCLIErrorInvalidServiceConfigFile,
		Description: "Printed when service-mode config is invalid.",
		Other:       "Invalid service config file",
	},
	{
		ID:          MessageIDCLIErrorInvalidTrustedProxyIPs,
		Description: "Printed when trusted proxy IP config is invalid.",
		Other:       "invalid --trusted-proxy-ips value",
	},
	{
		ID:          MessageIDCLIErrorInvalidWorkspaceConfig,
		Description: "Printed when workspace config is invalid.",
		Other:       "Invalid workspace configuration",
	},
	{
		ID:          MessageIDCLIErrorLeafWikiDaemonFailed,
		Description: "Printed when daemon startup fails.",
		Other:       "LeafWiki daemon failed",
	},
	{
		ID:          MessageIDCLIErrorLeafWikiStartupFailed,
		Description: "Printed when LeafWiki startup fails.",
		Other:       "LeafWiki startup failed",
	},
	{
		ID:          MessageIDCLIErrorPasswordResetFailed,
		Description: "Printed when admin password reset fails.",
		Other:       "Password reset failed",
	},
	{
		ID:          MessageIDCLIErrorProjectDaemonFailed,
		Description: "Printed when the project daemon command fails.",
		Other:       "Project daemon failed",
	},
	{
		ID:          MessageIDCLIErrorRuntimeRoleFailed,
		Description: "Printed when a runtime role command fails.",
		Other:       "Runtime role failed",
	},
	{
		ID:          MessageIDCLIErrorServiceConfigRequired,
		Description: "Printed when service mode requires a config file.",
		Other:       "Service config file is required for service mode",
	},
	{
		ID:          MessageIDCLIErrorStdioAuthAPIKeyConflict,
		Description: "Printed when disabled auth is combined with an API-key STDIO identity.",
		Other:       "disabled auth and API-key STDIO identity cannot be combined",
	},
	{
		ID:          MessageIDCLIErrorStdioAuthIdentityRequired,
		Description: "Printed when native STDIO is missing an auth mode.",
		Other:       "native STDIO requires either disabled auth or an API key",
	},
	{
		ID:          MessageIDCLIErrorStdoutReservedForMCPStdio,
		Description: "Printed when stdout logging is requested for MCP STDIO.",
		Other:       "stdout is reserved for MCP STDIO",
	},
	{
		ID:          MessageIDCLIStatusAdminPasswordReset,
		Description: "Printed after resetting the admin password.",
		Other:       "Admin password reset successfully.",
	},
	{
		ID:          MessageIDCLIStatusAdminPasswordValue,
		Description: "Printed with the newly generated admin password.",
		Other:       "New password for user {{.Arg0}}: {{.Arg1}}",
	},
	{
		ID:          MessageIDCLIStatusRemoveWorkspaceDescriptor,
		Description: "Printed when removing a workspace descriptor fails.",
		Other:       "leafwiki: remove workspace descriptor {{.Arg0}}: {{.Arg1}}",
	},
	{
		ID:          MessageIDCLIStatusUnknownCommand,
		Description: "Printed when an unknown positional command is provided.",
		Other:       "Unknown command: {{.Arg0}}",
	},
	{
		ID:          "validation.markdown.ambiguous_legacy_link",
		Description: "Returned when a markdown link is ambiguous under legacy resolution.",
		Other:       "Ambiguous legacy markdown link",
	},
	{
		ID:          "validation.markdown.broken_link",
		Description: "Returned when markdown content links to a missing page.",
		Other:       "Broken markdown link",
	},
	{
		ID:          "validation.markdown.duplicate_leafwiki_id",
		Description: "Returned when multiple markdown files declare the same page ID.",
		Other:       "Duplicate LeafWiki page ID",
	},
	{
		ID:          "validation.markdown.hidden_markdown_path",
		Description: "Returned when a markdown file is hidden from workspace discovery.",
		Other:       "Hidden markdown path",
	},
	{
		ID:          "validation.markdown.invalid_link",
		Description: "Returned when markdown content contains an invalid link.",
		Other:       "Invalid markdown link",
	},
	{
		ID:          "validation.markdown.invalid_path",
		Description: "Returned when a markdown path is invalid.",
		Other:       "Invalid markdown path",
	},
	{
		ID:          "validation.markdown.invalid_slug",
		Description: "Returned when markdown metadata contains an invalid slug.",
		Other:       "Invalid markdown slug",
	},
	{
		ID:          "validation.markdown.metadata_parse_error",
		Description: "Returned when markdown metadata cannot be parsed.",
		Other:       "Markdown metadata parse error",
	},
	{
		ID:          "validation.markdown.missing_asset",
		Description: "Returned when markdown content references a missing asset.",
		Other:       "Missing markdown asset",
	},
	{
		ID:          "validation.markdown.missing_title",
		Description: "Returned when markdown metadata is missing a title.",
		Other:       "Missing markdown title",
	},
	{
		ID:          "validation.markdown.non_canonical_link",
		Description: "Returned when markdown content uses a non-canonical link.",
		Other:       "Non-canonical markdown link",
	},
	{
		ID:          "validation.markdown.non_canonical_markdown_path",
		Description: "Returned when a markdown file path is non-canonical.",
		Other:       "Non-canonical markdown path",
	},
	{
		ID:          "validation.markdown.path_conflict",
		Description: "Returned when a markdown path belongs to another page.",
		Other:       "Markdown path conflict",
	},
	{
		ID:          "validation.markdown.reserved_metadata",
		Description: "Returned when markdown metadata uses reserved fields.",
		Other:       "Reserved markdown metadata",
	},
	{
		ID:          "validation.markdown.workspace_scan_error",
		Description: "Returned when workspace markdown scanning fails.",
		Other:       "Workspace markdown scan error",
	},
	{
		ID:          "validation.markdown.workspace_sync_error",
		Description: "Returned when workspace sync validation fails.",
		Other:       "Workspace sync error",
	},
	{
		ID:          "validation.markdown.workspace_sync_validation",
		Description: "Returned for workspace sync validation issues.",
		Other:       "Workspace sync validation issue",
	},
	{
		ID:          "warnings.link_rewrite.empty_destination",
		Description: "Returned when link rewriting would produce an empty destination.",
		Other:       "Skipped empty rewritten destination",
	},
	{
		ID:          "warnings.link_rewrite.unresolved_destination",
		Description: "Returned when link rewriting cannot resolve a destination.",
		Other:       "Skipped unresolved link destination",
	},
	{
		ID:          "warnings.link_rewrite.unsupported_syntax",
		Description: "Returned when link rewriting skips unsupported syntax.",
		Other:       "Skipped unsupported link syntax",
	},
	{
		ID:          MessageIDShellRunErrorAgentHookRequiresProvider,
		Description: "Printed when scripts/run.sh agent-hook mode is missing a provider.",
		Other:       "agent-hook requires a provider",
	},
	{
		ID:          MessageIDShellRunErrorArgumentContainsSpace,
		Description: "Printed when scripts/run.sh receives a combined MCP JSON argument containing a space.",
		Other:       "argument contains a space; MCP JSON args must split flags and values into separate args, for example \"--root-dir\", \"./wiki\", or use --root-dir=./wiki",
	},
	{
		ID:          MessageIDShellRunErrorConfigCannotCombine,
		Description: "Printed when scripts/run.sh --config is combined with another wrapper option.",
		Other:       "--config cannot be combined with",
	},
	{
		ID:          MessageIDShellRunErrorConfigRequiresPath,
		Description: "Printed when scripts/run.sh --config is missing a path.",
		Other:       "--config requires a path",
	},
	{
		ID:          MessageIDShellRunErrorDisableAuthAPIKeyConflict,
		Description: "Printed when disabled auth is combined with an MCP API key.",
		Other:       "--disable-auth cannot be combined with --api-key or LEAFWIKI_MCP_API_KEY",
	},
	{
		ID:          MessageIDShellRunErrorExecutableNotFound,
		Description: "Printed when scripts/run.sh receives a non-executable binary path.",
		Other:       "executable not found or not executable",
	},
	{
		ID:          MessageIDShellRunErrorExecutableNotFoundOnPath,
		Description: "Printed when scripts/run.sh cannot find a binary on PATH.",
		Other:       "executable not found on PATH",
	},
	{
		ID:          MessageIDShellRunErrorLeafWikiBinRequiresPath,
		Description: "Printed when scripts/run.sh --leafwiki-bin is missing a path.",
		Other:       "--leafwiki-bin requires a path",
	},
	{
		ID:          MessageIDShellRunErrorOptionRequiresArgument,
		Description: "Printed when scripts/run.sh option is missing a generic argument.",
		Other:       "requires an argument",
	},
	{
		ID:          MessageIDShellRunErrorOptionRequiresDuration,
		Description: "Printed when scripts/run.sh option is missing a duration.",
		Other:       "requires a duration",
	},
	{
		ID:          MessageIDShellRunErrorOptionRequiresPassword,
		Description: "Printed when scripts/run.sh option is missing a password.",
		Other:       "requires a password",
	},
}

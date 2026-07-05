package localization

import "github.com/nicksnyder/go-i18n/v2/i18n"

var registryMessagesValidation = []*i18n.Message{
	{
		ID:          messageIDMCPGetRevisionAssetDesc,
		Description: "Descriptor for the MCP revision asset tool.",
		Other:       "Read a revision asset as base64 content",
	},
	{
		ID:          messageIDMCPRestoreRevisionDesc,
		Description: "Descriptor for the MCP restore-revision tool.",
		Other:       "Restore a page revision",
	},
	{
		ID:          messageIDMCPPreviewRefactorDesc,
		Description: "Descriptor for the MCP refactor preview tool.",
		Other:       "Preview a page rename or move refactor",
	},
	{
		ID:          messageIDMCPApplyRefactorDesc,
		Description: "Descriptor for the MCP refactor apply tool.",
		Other:       "Apply a page rename or move refactor",
	},
	{
		ID:          messageIDMCPMovePageSuccess,
		Description: "Returned after a page is moved through MCP.",
		Other:       "Page moved",
	},
	{
		ID:          messageIDMCPSortPagesSuccess,
		Description: "Returned after child pages are sorted through MCP.",
		Other:       "Pages sorted successfully",
	},
	{
		ID:          messageIDAuthInvalidCreds,
		Description: "Returned when login credentials are not accepted.",
		Other:       "Invalid credentials",
	},
	{
		ID:          messageIDAuthEmailInvalid,
		Description: "Returned when an auth email field is not syntactically valid.",
		Other:       "Email is not valid",
	},
	{
		ID:          messageIDValidationFieldError,
		Description: "Fallback field validation message.",
		Other:       "Validation error",
	},
	{
		ID:          messageIDValidationBrandingNameReq,
		Description: "Returned when branding site name is empty.",
		Other:       "Site name must not be empty",
	},
	{
		ID:          messageIDValidationBrandingNameLong,
		Description: "Returned when branding site name is too long.",
		Other:       "Site name must not exceed the maximum length",
	},
	{
		ID:          messageIDValidationBrandingNameControl,
		Description: "Returned when branding site name has control characters.",
		Other:       "Site name contains invalid control characters",
	},
	{
		ID:          messageIDValidationPageTitleReq,
		Description: "Returned when a page title is empty.",
		Other:       "Title must not be empty",
	},
	{
		ID:          messageIDValidationPageKindReq,
		Description: "Returned when a page kind is missing.",
		Other:       "Kind must be specified",
	},
	{
		ID:          messageIDValidationPageKindInvalid,
		Description: "Returned when a page kind is invalid.",
		Other:       "Kind must be either 'page' or 'section'",
	},
	{
		ID:          messageIDValidationPageSlugInvalid,
		Description: "Returned when a page slug is invalid.",
		Other:       "Slug is invalid",
	},
	{
		ID:          messageIDValidationPagePathReq,
		Description: "Returned when a page path is empty.",
		Other:       "Path must not be empty",
	},
	{
		ID:          messageIDValidationPagePathInvalid,
		Description: "Returned when a page path is invalid.",
		Other:       "Path is invalid",
	},
	{
		ID:          messageIDValidationPageTagReq,
		Description: "Returned when a page tag is empty.",
		Other:       "Tag must not be empty",
	},
	{
		ID:          messageIDValidationPageTagWhitespace,
		Description: "Returned when a page tag has leading or trailing whitespace.",
		Other:       "Tag must not contain leading or trailing whitespace",
	},
	{
		ID:          messageIDValidationPageTagDuplicate,
		Description: "Returned when a page tag is duplicated.",
		Other:       "Tag must be unique",
	},
	{
		ID:          messageIDValidationPagePropertyKeyReq,
		Description: "Returned when a page property key is empty.",
		Other:       "Property key must not be empty",
	},
	{
		ID:          messageIDValidationPagePropertyKeyWhitespace,
		Description: "Returned when a page property key has leading or trailing whitespace.",
		Other:       "Property key must not contain leading or trailing whitespace",
	},
	{
		ID:          messageIDValidationPagePropertyKeyReserved,
		Description: "Returned when a page property key is reserved.",
		Other:       "Property key is reserved",
	},
	{
		ID:          messageIDValidationPagePropertyKeyReservedPrefix,
		Description: "Returned when a page property key uses a reserved prefix.",
		Other:       "Property key uses a reserved prefix",
	},
	{
		ID:          messageIDValidationAuthUsernameReq,
		Description: "Returned when username is empty.",
		Other:       "Username must not be empty",
	},
	{
		ID:          messageIDValidationAuthEmailReq,
		Description: "Returned when email is empty.",
		Other:       "Email must not be empty",
	},
	{
		ID:          messageIDValidationAuthPasswordReq,
		Description: "Returned when password is empty.",
		Other:       "Password must not be empty",
	},
	{
		ID:          messageIDValidationAuthPasswordShort,
		Description: "Returned when password is too short.",
		Other:       "Password must be at least 8 characters long",
	},
	{
		ID:          messageIDValidationAuthRoleInvalid,
		Description: "Returned when auth role is invalid.",
		Other:       "Invalid role",
	},
	{
		ID:          messageIDValidationAuthNewPasswordReq,
		Description: "Returned when new password is empty.",
		Other:       "New password must not be empty",
	},
	{
		ID:          messageIDValidationAuthNewPasswordShort,
		Description: "Returned when new password is too short.",
		Other:       "New password must be at least 8 characters long",
	},
	{
		ID:          messageIDValidationAuthOldPasswordIncorrect,
		Description: "Returned when old password is incorrect.",
		Other:       "Old password is incorrect",
	},
	{
		ID:          messageIDValidationAuthAPIKeyNameReq,
		Description: "Returned when API key name is empty.",
		Other:       "Name must not be empty",
	},
	{
		ID:          messageIDValidationAuthAPIKeyNameLong,
		Description: "Returned when API key name is too long.",
		Other:       "Name must be at most 80 characters long",
	},
	{
		ID:          messageIDValidationAuthCurrentPasswordReq,
		Description: "Returned when current password is empty.",
		Other:       "Current password must not be empty",
	},
	{
		ID:          messageIDValidationAuthCurrentPasswordIncorrect,
		Description: "Returned when current password is incorrect.",
		Other:       "Current password is incorrect",
	},
	{
		ID:          MessageIDCLIHelpUsage,
		Description: "Top-level LeafWiki CLI usage line.",
		Other:       "Usage: leafwiki [command]",
	},
	{
		ID:          MessageIDCLIHelpBody,
		Description: "Top-level LeafWiki CLI help body.",
		Other: `LeafWiki – lightweight selfhosted wiki 🌿

	Usage:
	leafwiki --jwt-secret <SECRET> --admin-password <PASSWORD> [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki --disable-auth [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki --mcp=stdio --disable-auth [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki agent-hook <codex|claude|cursor|unknown> [--host <HOST>] [--port <PORT>] [--data-dir <DIR>] [--root-dir <DIR>]
	leafwiki daemon
	leafwiki reset-admin-password
	leafwiki --help

	Service mode:
	leafwiki daemon reads ~/.leafwiki/leafwiki.yml and runs the install-wide wikid/frontd/workspaced runtime in the foreground.

	Options:
	--host             Host/IP address to bind the server to (default: 127.0.0.1)
	--port             Port to run the server on (default: 8080)
	--data-dir         Path to data directory (default: ./data)
	--root-dir         Path to managed markdown content directory (default: <data-dir>/root)
	--markdown-link-root-prefix Repository-root prefix for absolute Markdown links (for example /docs) (default: "")
	--admin-password   Initial admin password (used only if no admin exists)
	--jwt-secret       Secret for signing auth tokens (JWT) (required)
	--public-access    Allow public access to the wiki only with read access (default: false)
	--allow-insecure   Allow insecure HTTP connections (default: false)
	--access-token-timeout  Access token timeout duration (e.g. 24h, 15m) (default: 15m)
	--refresh-token-timeout Refresh token timeout duration (e.g. 168h, 7d) (default: 7d)
	--inject-code-in-header  Raw HTML/JS code injected into <head> tag (e.g., analytics, custom CSS) (default: "")
	                         WARNING: Use only with trusted code to avoid XSS vulnerabilities. No sanitization is performed.
	--custom-stylesheet      Path to a .css file inside the data dir, served publicly as /custom.css
	                         (or <base-path>/custom.css when --base-path is set) (default: "")
	--log-target             Log target: file, stderr, or stdout (default: file)
	--log-file               Log file path when --log-target=file; relative paths resolve under --data-dir
	                         (default: <data-dir>/.leafwiki/logs/leafwiki.log)
	--disable-auth                Disable authentication completely (default: false) (WARNING: only use in trusted networks!)
	--hide-link-metadata-section  Hide link metadata section in the frontend UI (default: false)
	--base-path                   URL prefix when served behind a reverse proxy (e.g. /wiki) (default: "")
	--max-asset-upload-size       Maximum size for asset uploads (for example 50MiB, 50MB, 52428800) (default: 50MiB)
	--enable-link-refactor        Enable the link refactoring dialog and rewrite flow (default: false)
	--mcp                         MCP transports: none, http, stdio, http,stdio, or stdio,http (default: none)
	--api-key                     Native STDIO MCP API key convenience flag; prefer LEAFWIKI_MCP_API_KEY
	--daemon-idle-timeout         Federated runtime idle timeout after the last session or presence record exits; 0 stops immediately (default: 10m)
	--enable-http-remote-user       Enable reverse-proxy authentication via HTTP header (default: false)
	--http-remote-user-header-name  HTTP header carrying the username from a trusted proxy (default: Remote-User)
	--trusted-proxy-ips             Comma-separated trusted proxy IPs/CIDRs (e.g. 127.0.0.1,172.18.0.0/16)
	--http-remote-user-logout-url   URL the frontend redirects to after logout in proxy-auth mode (default: "")
	--disable-request-log           Suppress per-request HTTP access log lines (default: false)
	--config                        Path to flat YAML config file; mutually exclusive with other CLI flags

	Environment variables:
	LEAFWIKI_HOST
	LEAFWIKI_PORT
	LEAFWIKI_DATA_DIR
	LEAFWIKI_ROOT_DIR
	LEAFWIKI_MARKDOWN_LINK_ROOT_PREFIX
	LEAFWIKI_JWT_SECRET
	LEAFWIKI_LOG_LEVEL
	LEAFWIKI_ADMIN_PASSWORD
	LEAFWIKI_PUBLIC_ACCESS
	LEAFWIKI_ALLOW_INSECURE
	LEAFWIKI_INJECT_CODE_IN_HEADER
	LEAFWIKI_CUSTOM_STYLESHEET
	LEAFWIKI_LOG_TARGET
	LEAFWIKI_LOG_FILE
	LEAFWIKI_ACCESS_TOKEN_TIMEOUT
	LEAFWIKI_REFRESH_TOKEN_TIMEOUT
	LEAFWIKI_DISABLE_AUTH
	LEAFWIKI_HIDE_LINK_METADATA_SECTION
	LEAFWIKI_BASE_PATH
	LEAFWIKI_MAX_ASSET_UPLOAD_SIZE
	LEAFWIKI_ENABLE_LINK_REFACTOR
	LEAFWIKI_MCP
	LEAFWIKI_MCP_API_KEY
	LEAFWIKI_DAEMON_IDLE_TIMEOUT
	LEAFWIKI_ENABLE_HTTP_REMOTE_USER
	LEAFWIKI_HTTP_REMOTE_USER_HEADER_NAME
	LEAFWIKI_TRUSTED_PROXY_IPS
	LEAFWIKI_HTTP_REMOTE_USER_LOGOUT_URL
	LEAFWIKI_DISABLE_REQUEST_LOG
	`,
	},
	{
		ID:          MessageIDShellRunUsage,
		Description: "Usage line for scripts/run.sh.",
		Other:       "Usage: scripts/run.sh <mcp|agent-hook> [options]",
	},
	{
		ID:          MessageIDShellRunHelpBody,
		Description: "Help body for scripts/run.sh.",
		Other: `Modes:
  mcp                     Run native LeafWiki MCP STDIO frontend
  agent-hook <provider>   Run one LeafWiki agent hook invocation

Options:
  --leafwiki-bin <path>     LeafWiki executable (default: leafwiki)
  --scheme <scheme>         Informational URL scheme for dry-run output (default: http)
  --host <host>             LeafWiki bind host (default: 127.0.0.1)
  --port <port>             LeafWiki port (default: 8080)
  --base-path <path>        LeafWiki base path, if any
  --data-dir <path>         LeafWiki data directory (default: ./.wiki)
  --root-dir <path>         LeafWiki root markdown directory (default: ./wiki)
  --markdown-link-root-prefix <path>
                            Repository-root Markdown href prefix, such as /docs
  --jwt-secret <secret>     JWT secret only when this run must bootstrap an auth-enabled owner
  --admin-password <pass>   Admin password only when this run must bootstrap an auth-enabled owner
  --disable-auth            Force disabled-auth STDIO identity
  --allow-insecure          Pass --allow-insecure to LeafWiki (default)
  --no-allow-insecure       Do not pass --allow-insecure
  --request-log             Keep LeafWiki request logs enabled
  --disable-request-log     Pass --disable-request-log to LeafWiki (default)
  --daemon-idle-timeout <d> Federated runtime idle timeout after the last session or presence record exits (default: 10m)
  --api-key <key>           Native STDIO API key; passed as LEAFWIKI_MCP_API_KEY
  --config <path>           Pass a LeafWiki YAML config file without wrapper defaults
  --server-arg <arg>        Extra argument passed to leafwiki; repeatable
  --dry-run                 Print the planned command without starting anything
  -h, --help                Show this help

Environment overrides use LEAFWIKI_RUN_MCP_* names matching the option names.
Use LEAFWIKI_RUN_MCP_API_KEY or LEAFWIKI_MCP_API_KEY to provide the native
STDIO API key without putting the secret in the child command line.`,
	},
	{
		ID:          MessageIDShellRunErrorPrefix,
		Description: "Error prefix for scripts/run.sh.",
		Other:       "Error:",
	},
	{
		ID:          MessageIDShellRunDryRunMCPConfig,
		Description: "Dry-run status when scripts/run.sh starts MCP from YAML config.",
		Other:       "Would run LeafWiki with YAML config for MCP",
	},
	{
		ID:          MessageIDShellRunDryRunMCPNative,
		Description: "Dry-run status when scripts/run.sh starts native MCP STDIO.",
		Other:       "Would run LeafWiki native MCP STDIO",
	},
	{
		ID:          MessageIDShellRunDryRunSTDIOAttach,
		Description: "Dry-run detail for native MCP STDIO descriptor attach.",
		Other:       "STDIO attach: descriptor-first attach via <data-dir>/.leafwiki/project-daemon.json; wikid ensure/control for missing or stale descriptors",
	},
	{
		ID:          MessageIDShellRunDryRunAgentHook,
		Description: "Dry-run status when scripts/run.sh starts an agent hook.",
		Other:       "Would run LeafWiki agent hook",
	},
	{
		ID:          MessageIDShellRunDryRunHTTPConfig,
		Description: "Dry-run HTTP UI status for YAML-configured scripts/run.sh.",
		Other:       "HTTP UI: configured by",
	},
	{
		ID:          MessageIDShellRunDryRunHTTPURL,
		Description: "Dry-run HTTP UI status for URL-configured scripts/run.sh.",
		Other:       "HTTP UI:",
	},
}

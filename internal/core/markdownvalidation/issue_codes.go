package markdownvalidation

import "strings"

type IssueSeverity string

func (severity IssueSeverity) String() string {
	return string(severity)
}

func (severity IssueSeverity) Normalize(defaultSeverity IssueSeverity) IssueSeverity {
	normalized := IssueSeverity(strings.TrimSpace(string(severity)))
	if normalized == "" {
		return defaultSeverity
	}
	return normalized
}

const (
	IssueSeverityError   IssueSeverity = "error"
	IssueSeverityWarning IssueSeverity = "warning"
)

type IssueCode string

func (code IssueCode) String() string {
	return string(code)
}

func (code IssueCode) Normalize(defaultCode IssueCode) IssueCode {
	normalized := IssueCode(strings.TrimSpace(string(code)))
	if normalized == "" {
		return defaultCode
	}
	return normalized
}

const (
	IssueCodeInvalidPath              IssueCode = "invalid_path"
	IssueCodePathConflict             IssueCode = "path_conflict"
	IssueCodeMetadataParseError       IssueCode = "metadata_parse_error"
	IssueCodeWorkspaceSyncValidation  IssueCode = "workspace_sync_validation"
	IssueCodeWorkspaceSyncError       IssueCode = "workspace_sync_error"
	IssueCodeWorkspaceScanError       IssueCode = "workspace_scan_error"
	IssueCodeInvalidSlug              IssueCode = "invalid_slug"
	IssueCodeHiddenMarkdownPath       IssueCode = "hidden_markdown_path"
	IssueCodeDuplicateLeafwikiID      IssueCode = "duplicate_leafwiki_id"
	IssueCodeMissingTitle             IssueCode = "missing_title"
	IssueCodeReservedMetadata         IssueCode = "reserved_metadata"
	IssueCodeMissingAsset             IssueCode = "missing_asset"
	IssueCodeInvalidLink              IssueCode = "invalid_link"
	IssueCodeBrokenLink               IssueCode = "broken_link"
	IssueCodeNonCanonicalLink         IssueCode = "non_canonical_link"
	IssueCodeNonCanonicalMarkdownPath IssueCode = "non_canonical_markdown_path"
	IssueCodeAmbiguousLegacyLink      IssueCode = "ambiguous_legacy_link"
)

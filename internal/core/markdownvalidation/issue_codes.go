package markdownvalidation

import (
	"strings"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

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

func (code IssueCode) MessageID() sharederrors.MessageID {
	normalized := code.Normalize(IssueCodeWorkspaceSyncValidation)
	if messageID, ok := issueCodeMessageIDs[normalized]; ok {
		return messageID
	}
	return issueCodeMessageIDs[IssueCodeWorkspaceSyncValidation]
}

var issueCodeMessageIDs = map[IssueCode]sharederrors.MessageID{
	IssueCodeInvalidPath:              MessageIDInvalidPath,
	IssueCodePathConflict:             MessageIDPathConflict,
	IssueCodeMetadataParseError:       MessageIDMetadataParseError,
	IssueCodeWorkspaceSyncValidation:  MessageIDWorkspaceSyncValidation,
	IssueCodeWorkspaceSyncError:       MessageIDWorkspaceSyncError,
	IssueCodeWorkspaceScanError:       MessageIDWorkspaceScanError,
	IssueCodeInvalidSlug:              MessageIDInvalidSlug,
	IssueCodeHiddenMarkdownPath:       MessageIDHiddenMarkdownPath,
	IssueCodeDuplicateLeafwikiID:      MessageIDDuplicateLeafwikiID,
	IssueCodeMissingTitle:             MessageIDMissingTitle,
	IssueCodeReservedMetadata:         MessageIDReservedMetadata,
	IssueCodeMissingAsset:             MessageIDMissingAsset,
	IssueCodeInvalidLink:              MessageIDInvalidLink,
	IssueCodeBrokenLink:               MessageIDBrokenLink,
	IssueCodeNonCanonicalLink:         MessageIDNonCanonicalLink,
	IssueCodeNonCanonicalMarkdownPath: MessageIDNonCanonicalMarkdownPath,
	IssueCodeAmbiguousLegacyLink:      MessageIDAmbiguousLegacyLink,
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

const (
	MessageIDInvalidPath              sharederrors.MessageID = "validation.markdown.invalid_path"
	MessageIDPathConflict             sharederrors.MessageID = "validation.markdown.path_conflict"
	MessageIDMetadataParseError       sharederrors.MessageID = "validation.markdown.metadata_parse_error"
	MessageIDWorkspaceSyncValidation  sharederrors.MessageID = "validation.markdown.workspace_sync_validation"
	MessageIDWorkspaceSyncError       sharederrors.MessageID = "validation.markdown.workspace_sync_error"
	MessageIDWorkspaceScanError       sharederrors.MessageID = "validation.markdown.workspace_scan_error"
	MessageIDInvalidSlug              sharederrors.MessageID = "validation.markdown.invalid_slug"
	MessageIDHiddenMarkdownPath       sharederrors.MessageID = "validation.markdown.hidden_markdown_path"
	MessageIDDuplicateLeafwikiID      sharederrors.MessageID = "validation.markdown.duplicate_leafwiki_id"
	MessageIDMissingTitle             sharederrors.MessageID = "validation.markdown.missing_title"
	MessageIDReservedMetadata         sharederrors.MessageID = "validation.markdown.reserved_metadata"
	MessageIDMissingAsset             sharederrors.MessageID = "validation.markdown.missing_asset"
	MessageIDInvalidLink              sharederrors.MessageID = "validation.markdown.invalid_link"
	MessageIDBrokenLink               sharederrors.MessageID = "validation.markdown.broken_link"
	MessageIDNonCanonicalLink         sharederrors.MessageID = "validation.markdown.non_canonical_link"
	MessageIDNonCanonicalMarkdownPath sharederrors.MessageID = "validation.markdown.non_canonical_markdown_path"
	MessageIDAmbiguousLegacyLink      sharederrors.MessageID = "validation.markdown.ambiguous_legacy_link"
)

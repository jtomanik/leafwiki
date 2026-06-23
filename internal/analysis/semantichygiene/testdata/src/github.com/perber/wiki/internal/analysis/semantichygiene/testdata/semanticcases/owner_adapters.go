package semanticcases

import "strings"

type IssueSeverity string

func NewAPIKeyIDUnchecked(raw string) APIKeyID {
	return APIKeyID(raw)
}

func NewUserIDUnchecked(raw string) UserID {
	return UserID(raw)
}

func (code IssueCode) Normalize(defaultCode IssueCode) IssueCode {
	normalized := IssueCode(strings.TrimSpace(string(code)))
	if normalized == "" {
		return defaultCode
	}
	return normalized
}

func (severity IssueSeverity) Normalize(defaultSeverity IssueSeverity) IssueSeverity {
	normalized := IssueSeverity(strings.TrimSpace(string(severity)))
	if normalized == "" {
		return defaultSeverity
	}
	return normalized
}

func (id WorkspaceID) HTTPHeaderValue() string {
	return string(id)
}

func (id WorkspaceID) StorageKey() string {
	return string(id)
}

func (id WorkspaceID) Validate() bool {
	raw := string(id)
	return strings.TrimSpace(raw) == raw
}

func (name AssetName) Clean() AssetName {
	return AssetName(strings.TrimSpace(string(name)))
}

package links

import "github.com/perber/wiki/internal/core/tree"

// TargetKind is the semantic link-target kind stored in the links index.
type TargetKind string

const (
	TargetKindPage    TargetKind = "page"
	TargetKindSection TargetKind = "section"
	TargetKindUnknown TargetKind = "unknown"

	nonCanonicalPageStoredTarget TargetKind = "non_canonical_page"
)

const (
	defaultStoredTargetKind = TargetKindPage
	sectionStoredTargetKind = TargetKindSection
	unknownStoredTargetKind = TargetKindUnknown
)

func TargetKindFromNodeKind(kind tree.NodeKind) TargetKind {
	if kind == tree.NodeKindSection {
		return TargetKindSection
	}
	return TargetKindPage
}

func (kind TargetKind) Stored() TargetKind {
	switch kind {
	case TargetKindSection:
		return TargetKindSection
	case TargetKindUnknown:
		return TargetKindUnknown
	case nonCanonicalPageStoredTarget:
		return nonCanonicalPageStoredTarget
	default:
		return TargetKindPage
	}
}

func (kind TargetKind) IsZero() bool {
	return kind == ""
}

func storedTargetKind(kind TargetKind) TargetKind {
	return kind.Stored()
}

func MarkdownSourceKindFromNodeKind(kind tree.NodeKind) MarkdownSourceKind {
	if kind == tree.NodeKindSection {
		return MarkdownSourceKindSection
	}
	return MarkdownSourceKindPage
}

func (kind MarkdownSourceKind) TargetKind() TargetKind {
	if kind == MarkdownSourceKindSection {
		return TargetKindSection
	}
	return TargetKindPage
}

package tree

import (
	"encoding/json"
	"errors"

	"github.com/perber/wiki/internal/core/identity"
	"github.com/perber/wiki/internal/core/treemigration"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

type nodeKindParseState string

const (
	nodeKindRecognized nodeKindParseState = "recognized"
	nodeKindRejected   nodeKindParseState = "rejected"
)

type nodeKindParseObservation struct {
	Kind  NodeKind
	State nodeKindParseState
}

type pageVersionCheckState uint8

const (
	pageVersionChecked pageVersionCheckState = iota
	pageVersionUncheckedState
)

type semanticWrapperBehavior struct {
	VersionState            pageVersionCheckState
	RouteMarkdownPagePath   MarkdownPath
	RouteWorkspacePagePath  WorkspaceSourcePath
	RouteWorkspaceIndexPath WorkspaceSourcePath
	MarkdownRoutePath       RoutePath
	MarkdownSourceDir       MarkdownPath
	WorkspaceSourceDir      WorkspaceSourcePath
	SlugRoutePath           RoutePath
	SlugKey                 SlugKey
	AssetClean              AssetName
}

func observeNodeKindValueParse(kind NodeKind) nodeKindParseObservation {
	encoded, err := json.Marshal(kind)
	if err != nil {
		return nodeKindParseObservation{State: nodeKindRejected}
	}
	var raw string
	if err := json.Unmarshal(encoded, &raw); err != nil {
		return nodeKindParseObservation{State: nodeKindRejected}
	}
	kind, ok := ParseNodeKind(raw)
	if !ok {
		return nodeKindParseObservation{State: nodeKindRejected}
	}
	return nodeKindParseObservation{Kind: kind, State: nodeKindRecognized}
}

func matchNodeKindValueParse(kind NodeKind, state nodeKindParseState) types.GomegaMatcher {
	return WithTransform(observeNodeKindValueParse, gstruct.MatchAllFields(gstruct.Fields{
		"Kind":  Equal(kind),
		"State": Equal(state),
	}))
}

func observePageVersionCheckState(version PageVersion) pageVersionCheckState {
	if version.IsUnchecked() {
		return pageVersionUncheckedState
	}
	return pageVersionChecked
}

func observeSemanticWrapperBehavior(
	version PageVersion,
	route RoutePath,
	markdownPath MarkdownPath,
	workspacePath WorkspaceSourcePath,
	slug Slug,
	assetName AssetName,
) semanticWrapperBehavior {
	return semanticWrapperBehavior{
		VersionState:            observePageVersionCheckState(version),
		RouteMarkdownPagePath:   route.MarkdownPagePath(),
		RouteWorkspacePagePath:  route.WorkspaceSourcePath(NodeKindPage),
		RouteWorkspaceIndexPath: route.WorkspaceSourcePath(NodeKindSection),
		MarkdownRoutePath:       markdownPath.RoutePath(),
		MarkdownSourceDir:       markdownPath.SourceDir(),
		WorkspaceSourceDir:      workspacePath.Dir(),
		SlugRoutePath:           slug.RoutePath(),
		SlugKey:                 slug.SlugKey(),
		AssetClean:              assetName.Clean(),
	}
}

func matchSemanticWrapperBehavior(expected semanticWrapperBehavior) types.GomegaMatcher {
	return gstruct.MatchAllFields(gstruct.Fields{
		"VersionState":            Equal(expected.VersionState),
		"RouteMarkdownPagePath":   Equal(expected.RouteMarkdownPagePath),
		"RouteWorkspacePagePath":  Equal(expected.RouteWorkspacePagePath),
		"RouteWorkspaceIndexPath": Equal(expected.RouteWorkspaceIndexPath),
		"MarkdownRoutePath":       Equal(expected.MarkdownRoutePath),
		"MarkdownSourceDir":       Equal(expected.MarkdownSourceDir),
		"WorkspaceSourceDir":      Equal(expected.WorkspaceSourceDir),
		"SlugRoutePath":           Equal(expected.SlugRoutePath),
		"SlugKey":                 Equal(expected.SlugKey),
		"AssetClean":              Equal(expected.AssetClean),
	})
}

type treeDomainErrorContract struct {
	Sentinel error
	NodeID   PageID
	Kind     NodeKind
	Path     string
	Reason   string
	Op       string
	Resource string
	ID       PageID
	From     NodeKind
	To       NodeKind
}

func treeDomainError(err error) treeDomainErrorContract {
	_ = err.Error()
	switch typed := err.(type) {
	case *DriftError:
		return treeDomainErrorContract{
			Sentinel: errors.Unwrap(typed),
			NodeID:   typed.NodeID,
			Kind:     typed.Kind,
			Path:     typed.Path,
			Reason:   typed.Reason,
		}
	case *InvalidOpError:
		return treeDomainErrorContract{
			Sentinel: errors.Unwrap(typed),
			Op:       typed.Op,
			Reason:   typed.Reason,
		}
	case *PageAlreadyExistsError:
		return treeDomainErrorContract{
			Sentinel: errors.Unwrap(typed),
			Path:     typed.Path,
		}
	case *NotFoundError:
		return treeDomainErrorContract{
			Sentinel: errors.Unwrap(typed),
			Resource: typed.Resource,
			ID:       typed.ID,
			Path:     typed.Path,
		}
	case *ConvertNotAllowedError:
		return treeDomainErrorContract{
			Sentinel: errors.Unwrap(typed),
			From:     typed.From,
			To:       typed.To,
			Reason:   typed.Reason,
		}
	default:
		return treeDomainErrorContract{}
	}
}

func matchTreeDomainError(fields gstruct.Fields) types.GomegaMatcher {
	return WithTransform(treeDomainError, gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

var _ = ginkgo.Describe("route path parsing", ginkgo.Label("unit"), func() {
	ginkgo.It("returns semantic route path values", func() {
		routePath, err := ParseRoutePath("docs/guide")
		Expect(err).To(Succeed())
		Expect(routePath).To(Equal(newFixtureRoutePath("docs/guide")))

	})
})

var _ = ginkgo.Describe("semantic page value wrappers", ginkgo.Label("unit"), func() {
	ginkgo.It("keep page identity, versions, slugs, and markdown paths distinct", func() {
		pageID := newFixturePageID("page-1")
		version := newFixturePageVersion("v2")
		slug := newFixtureSlug("guide")
		markdownPath := newFixtureMarkdownPath("docs/guide.md")

		Expect(pageID).To(Equal(newFixturePageID("page-1")))
		Expect(version).To(Equal(newFixturePageVersion("v2")))
		Expect(slug).To(Equal(newFixtureSlug("guide")))
		Expect(markdownPath).To(Equal(newFixtureMarkdownPath("docs/guide.md")))

	})

	ginkgo.It("exposes semantic wrapper behavior through typed path and asset operations", func() {
		route := newFixtureRoutePath("docs/guide")
		assetName := newFixtureAssetName(" logo.png ")

		Expect(observeSemanticWrapperBehavior(
			newFixturePageVersion("v2"),
			route,
			newFixtureMarkdownPath("docs/guide.md"),
			newFixtureWorkspaceSourcePath("docs/guide.md"),
			newFixtureSlug("guide"),
			assetName,
		)).To(matchSemanticWrapperBehavior(semanticWrapperBehavior{
			VersionState:            pageVersionChecked,
			RouteMarkdownPagePath:   newFixtureMarkdownPath("docs/guide.md"),
			RouteWorkspacePagePath:  newFixtureWorkspaceSourcePath("docs/guide.md"),
			RouteWorkspaceIndexPath: newFixtureWorkspaceSourcePath("docs/guide"),
			MarkdownRoutePath:       route,
			MarkdownSourceDir:       newFixtureMarkdownPath("docs"),
			WorkspaceSourceDir:      newFixtureWorkspaceSourcePath("docs"),
			SlugRoutePath:           newFixtureRoutePath("guide"),
			SlugKey:                 newFixtureSlug("guide").SlugKey(),
			AssetClean:              newFixtureAssetName("logo.png"),
		}))
	})
})

var _ = ginkgo.Describe("node kind parsing", ginkgo.Label("unit"), func() {
	ginkgo.It("accepts known node kinds and rejects unknown ones", func() {
		unknownKind := treeNodeKind(treemigration.NodeKindUnknown)

		Expect(NodeKindPage).To(matchNodeKindValueParse(NodeKindPage, nodeKindRecognized))
		Expect(NodeKindSection).To(matchNodeKindValueParse(NodeKindSection, nodeKindRecognized))
		Expect(unknownKind).To(matchNodeKindValueParse(unknownKind, nodeKindRejected))
	})

	ginkgo.It("maps tree and migration node kind boundaries conservatively", func() {
		Expect(migrationNodeKind(NodeKindPage)).To(Equal(treemigration.NodeKindPage))
		Expect(migrationNodeKind(NodeKindSection)).To(Equal(treemigration.NodeKindSection))
		Expect(migrationNodeKind(treeNodeKind(treemigration.NodeKindUnknown))).To(Equal(treemigration.NodeKindUnknown))

		Expect(treeNodeKind(treemigration.NodeKindPage)).To(Equal(NodeKindPage))
		Expect(treeNodeKind(treemigration.NodeKindSection)).To(Equal(NodeKindSection))
		Expect(treeNodeKind(treemigration.NodeKindUnknown)).To(BeEmpty())
	})
})

var _ = ginkgo.Describe("tree domain errors", ginkgo.Label("unit"), func() {
	ginkgo.It("preserve typed fields and sentinel identities for callers", func() {
		drift := &DriftError{
			NodeID: newFixturePageID("page-1"),
			Kind:   NodeKindPage,
			Path:   newFixtureRoutePath("docs/guide").WikiPath(),
			Reason: "hash mismatch",
		}
		invalid := &InvalidOpError{Op: "move", Reason: "root node"}
		existing := &PageAlreadyExistsError{Path: newFixtureRoutePath("docs/guide").WikiPath()}
		missing := &NotFoundError{Resource: "page", ID: newFixturePageID("missing")}
		blocked := &ConvertNotAllowedError{From: NodeKindPage, To: NodeKindSection, Reason: "has content"}

		Expect(drift).To(matchTreeDomainError(gstruct.Fields{
			"Sentinel": BeIdenticalTo(ErrDrift),
			"NodeID":   Equal(newFixturePageID("page-1")),
			"Kind":     Equal(NodeKindPage),
			"Path":     Equal(newFixtureRoutePath("docs/guide").WikiPath()),
			"Reason":   Equal("hash mismatch"),
		}))
		Expect(invalid).To(matchTreeDomainError(gstruct.Fields{
			"Sentinel": BeIdenticalTo(ErrInvalidOperation),
			"Op":       Equal("move"),
			"Reason":   Equal("root node"),
		}))
		Expect(existing).To(matchTreeDomainError(gstruct.Fields{
			"Sentinel": BeIdenticalTo(ErrPageAlreadyExists),
			"Path":     Equal(newFixtureRoutePath("docs/guide").WikiPath()),
		}))
		Expect(missing).To(matchTreeDomainError(gstruct.Fields{
			"Sentinel": BeIdenticalTo(ErrPageNotFound),
			"Resource": Equal("page"),
			"ID":       Equal(newFixturePageID("missing")),
		}))
		Expect(blocked).To(matchTreeDomainError(gstruct.Fields{
			"Sentinel": BeIdenticalTo(ErrConvertNotAllowed),
			"From":     Equal(NodeKindPage),
			"To":       Equal(NodeKindSection),
			"Reason":   Equal("has content"),
		}))
	})
})

var _ = ginkgo.Describe("page version bypass sentinel", ginkgo.Label("unit"), func() {
	ginkgo.It("stays internal to tree operations", func() {
		Expect(pageVersionUnchecked).To(matchPageVersionBypassState(pageVersionBypassUnchecked))
		got := newFixturePageVersion(versionUnchecked)
		Expect(got).To(matchPageVersionBypassState(pageVersionNormal))
		Expect(got).To(BeEmpty())

	})
})

var _ = ginkgo.Describe("tree service unchecked-version boundaries", ginkgo.Label("unit"), func() {
	ginkgo.It("exposes constrained operations for internal version bypasses", func() {
		var _ func(*TreeService, UserID, PageID, bool) error = (*TreeService).DeleteNodeUncheckedVersion
		var _ func(*TreeService, UserID, PageID, string, Slug, *string, bool) error = (*TreeService).UpdateNodeUncheckedVersion
		var _ func(*TreeService, UserID, PageID, string, Slug, *string) error = (*TreeService).UpdateNodeReplacingMetadataUncheckedVersion
		var _ func(*TreeService, UserID, PageID, PageID) error = (*TreeService).MoveNodeUncheckedVersion
		var _ func(*TreeService, UserID, PageID, NodeKind) error = (*TreeService).ConvertNodeUncheckedVersion

	})
})

var _ = ginkgo.Describe("tree identity aliases", ginkgo.Label("unit"), func() {
	ginkgo.It("remain assignable to neutral identity types", func() {
		var _ identity.UserID = newFixtureUserID("user-1")
		var _ identity.RevisionID = newFixtureRevisionID("rev-1")
		var _ identity.CommitHash = newFixtureCommitHash("abc123")

	})
})

var _ = ginkgo.Describe("workspace source paths", ginkgo.Label("unit"), func() {
	ginkgo.It("use semantic path values on nodes and markdown routes", func() {
		sourcePath := newFixtureWorkspaceSourcePath("Plans/Agent Hooks.PLAN.md")
		Expect(sourcePath).To(Equal(newFixtureWorkspaceSourcePath("Plans/Agent Hooks.PLAN.md")))

		node := PageNode{WorkspaceSourcePath: sourcePath}
		var _ WorkspaceSourcePath = node.WorkspaceSourcePath

		route := WorkspaceMarkdownRoute{SourcePath: sourcePath}
		var _ WorkspaceSourcePath = route.SourcePath

	})
})

var _ = ginkgo.Describe("core page identity fields", ginkgo.Label("unit"), func() {
	ginkgo.It("use semantic page and user identity types", func() {
		node := PageNode{
			ID: newFixturePageID("page-1"),
			Metadata: PageMetadata{
				CreatorID:    newFixtureUserID("user-1"),
				LastAuthorID: newFixtureUserID("user-2"),
			},
		}
		var _ PageID = node.ID
		var _ UserID = node.Metadata.CreatorID
		var _ UserID = node.Metadata.LastAuthorID

		var _ func(*PageNode, PageID, bool) bool = (*PageNode).IsChildOf

	})
})

var _ = ginkgo.Describe("tree service write boundaries", ginkgo.Label("unit"), func() {
	ginkgo.It("accept semantic identity values at mutation boundaries", func() {
		var _ func(*TreeService, UserID, *PageID, string, Slug, *NodeKind) (*PageID, error) = (*TreeService).CreateNode
		var _ func(*TreeService, UserID, PageID, *PageID, string, Slug, NodeKind, string, PageMetadata) (*Page, error) = (*TreeService).RestoreNode
		var _ func(*TreeService, UserID, []BulkContentUpdate) []error = (*TreeService).BulkUpdateContent

		update := BulkContentUpdate{ID: newFixturePageID("page-1"), Content: "content"}
		var _ PageID = update.ID

	})
})

var _ = ginkgo.Describe("tree service read boundaries", ginkgo.Label("unit"), func() {
	ginkgo.It("return and accept semantic identity values at lookup boundaries", func() {
		var _ func(*TreeService, PageID) (*Page, error) = (*TreeService).GetPage
		var _ func(*TreeService, PageID) (string, error) = (*TreeService).ReadPageRaw
		var _ func(*TreeService, []PageID) ([]*Page, []error) = (*TreeService).GetPages
		var _ func(*TreeService, PageID) (*PageNode, error) = (*TreeService).FindPageByID
		var _ func(*TreeService, PageID) (*PermalinkTarget, error) = (*TreeService).ResolvePermalinkTarget
		var _ func(*TreeService, RoutePath) (*Page, error) = (*TreeService).FindPageByRoutePath
		var _ func(*TreeService, RoutePath, NodeKind) (*Page, error) = (*TreeService).FindPageByRoutePathAndKind
		var _ func(*TreeService, RoutePath) (*PathLookup, error) = (*TreeService).LookupPagePath
		var _ func(*TreeService, RoutePath, NodeKind) (*PathLookup, error) = (*TreeService).LookupPagePathForKind

	})
})

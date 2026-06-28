package pages

import (
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/links"
)

type createPageTree interface {
	FindPageByID(tree.PageID) (*tree.PageNode, error)
	CreateNode(tree.UserID, *tree.PageID, string, tree.Slug, *tree.NodeKind) (*tree.PageID, error)
	GetPage(tree.PageID) (*tree.Page, error)
}

type updatePageTree interface {
	GetPage(tree.PageID) (*tree.Page, error)
	UpdateNode(tree.UserID, tree.PageID, string, tree.Slug, *string, tree.PageVersion, bool) error
	GetPages([]tree.PageID) ([]*tree.Page, []error)
}

type deletePageTree interface {
	GetPage(tree.PageID) (*tree.Page, error)
	GetPages([]tree.PageID) ([]*tree.Page, []error)
	DeleteNode(tree.UserID, tree.PageID, bool, tree.PageVersion) error
}

type movePageTree interface {
	GetPage(tree.PageID) (*tree.Page, error)
	MoveNode(tree.UserID, tree.PageID, tree.PageID, tree.PageVersion) error
	GetPages([]tree.PageID) ([]*tree.Page, []error)
}

type convertPageTree interface {
	GetPage(tree.PageID) (*tree.Page, error)
	ConvertNode(tree.UserID, tree.PageID, tree.NodeKind, tree.PageVersion) error
}

type copyPageTree interface {
	GetPage(tree.PageID) (*tree.Page, error)
	CreateNode(tree.UserID, *tree.PageID, string, tree.Slug, *tree.NodeKind) (*tree.PageID, error)
	DeleteNodeUncheckedVersion(tree.UserID, tree.PageID, bool) error
	UpdateNodeUncheckedVersion(tree.UserID, tree.PageID, string, tree.Slug, *string, bool) error
}

type ensurePathTree interface {
	LookupPagePath(tree.RoutePath) (*tree.PathLookup, error)
	GetPage(tree.PageID) (*tree.Page, error)
	EnsurePagePath(tree.UserID, tree.RoutePath, string, *tree.NodeKind) (*tree.EnsurePathResult, error)
	GetPages([]tree.PageID) ([]*tree.Page, []error)
}

type refactorPreviewTree interface {
	GetPage(tree.PageID) (*tree.Page, error)
}

type refactorApplyTree interface {
	updatePageTree
	movePageTree
	BulkUpdateContent(tree.UserID, []tree.BulkContentUpdate) []error
}

type refactorLinkFinder interface {
	GetRefactorMatchesForPrefixAndKind(tree.RoutePath, tree.NodeKind) ([]links.RefactorLinkMatch, error)
}

type pageAssetCopier interface {
	CopyAllAssets(*tree.PageNode, *tree.PageNode) error
	DeleteAllAssetsForPage(*tree.PageNode) error
}

type pageAssetDeleter interface {
	DeleteAllAssetsForPage(*tree.PageNode) error
}

package importer

import (
	"mime/multipart"

	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"
)

type ImporterWiki interface {
	TreeHash() string
	LookupPagePath(path tree.RoutePath) (*tree.PathLookup, error)
	LookupPagePathForKind(path tree.RoutePath, kind tree.NodeKind) (*tree.PathLookup, error)
	EnsurePath(userID tree.UserID, targetPath tree.RoutePath, title string, kind *tree.NodeKind) (*tree.Page, error)
	UpdatePage(userID tree.UserID, id tree.PageID, title string, slug tree.Slug, content *string, kind *tree.NodeKind) (*tree.Page, error)
	UploadAsset(userID tree.UserID, pageID tree.PageID, file multipart.File, filename tree.AssetName, byteCap shared.MaxBytes) (string, error)
}

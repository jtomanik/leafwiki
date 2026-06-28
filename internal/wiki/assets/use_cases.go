package assets

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"

	coreassets "github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/shared"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

// ─── UploadAssetUseCase ──────────────────────────────────────────────────────

type UploadAssetInput struct {
	UserID   tree.UserID
	PageID   tree.PageID
	File     multipart.File
	Filename tree.AssetName
	ByteCap  shared.MaxBytes
}

type UploadAssetOutput struct {
	URL string
}

type UploadAssetUseCase struct {
	tree  *tree.TreeService
	asset *coreassets.AssetService
	log   *slog.Logger
}

func NewUploadAssetUseCase(t *tree.TreeService, a *coreassets.AssetService, log *slog.Logger) *UploadAssetUseCase {
	return &UploadAssetUseCase{tree: t, asset: a, log: log}
}

func (uc *UploadAssetUseCase) Execute(_ context.Context, in UploadAssetInput) (*UploadAssetOutput, error) {
	page, err := uc.tree.FindPageByID(in.PageID)
	if err != nil {
		if errors.Is(err, tree.ErrPageNotFound) {
			return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetPageNotFound, err, in.PageID.MetadataValue())
		}
		return nil, err
	}
	url, err := uc.asset.SaveAssetForPage(page, in.File, in.Filename, in.ByteCap)
	if err != nil {
		return nil, err
	}
	return &UploadAssetOutput{URL: url}, nil
}

// ─── ListAssetsUseCase ───────────────────────────────────────────────────────

type ListAssetsInput struct {
	PageID tree.PageID
}

type ListAssetsOutput struct {
	Files []string
}

type ListAssetsUseCase struct {
	tree  *tree.TreeService
	asset assetLister
}

type assetLister interface {
	ListAssetsForPage(page *tree.PageNode) ([]string, error)
}

func NewListAssetsUseCase(t *tree.TreeService, a *coreassets.AssetService) *ListAssetsUseCase {
	return &ListAssetsUseCase{tree: t, asset: a}
}

func (uc *ListAssetsUseCase) Execute(_ context.Context, in ListAssetsInput) (*ListAssetsOutput, error) {
	page, err := uc.tree.FindPageByID(in.PageID)
	if err != nil {
		if errors.Is(err, tree.ErrPageNotFound) {
			return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetPageNotFound, err, in.PageID.MetadataValue())
		}
		return nil, err
	}
	files, err := uc.asset.ListAssetsForPage(page)
	if err != nil {
		return nil, err
	}
	return &ListAssetsOutput{Files: files}, nil
}

// ─── GetAssetUseCase ────────────────────────────────────────────────────────

type GetAssetInput struct {
	PageID   tree.PageID
	Filename tree.AssetName
}

type GetAssetOutput struct {
	Filename tree.AssetName
	MIMEType string
	Content  []byte
}

type GetAssetUseCase struct {
	tree  *tree.TreeService
	asset *coreassets.AssetService
}

func NewGetAssetUseCase(t *tree.TreeService, a *coreassets.AssetService) *GetAssetUseCase {
	return &GetAssetUseCase{tree: t, asset: a}
}

func (uc *GetAssetUseCase) Execute(_ context.Context, in GetAssetInput) (*GetAssetOutput, error) {
	page, err := uc.tree.FindPageByID(in.PageID)
	if err != nil {
		if errors.Is(err, tree.ErrPageNotFound) {
			return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetPageNotFound, err, in.PageID.MetadataValue())
		}
		return nil, err
	}
	filename := in.Filename.Clean()
	content, err := uc.asset.ReadAssetForPage(page, filename)
	if err != nil {
		return nil, err
	}
	return &GetAssetOutput{
		Filename: filename,
		MIMEType: DetectAssetMIMEType(filename.Filename(), content),
		Content:  content,
	}, nil
}

func DetectAssetMIMEType(filename string, content []byte) string {
	if mimeType := mime.TypeByExtension(filepath.Ext(filename)); mimeType != "" {
		return mimeType
	}
	return http.DetectContentType(content)
}

// ─── RenameAssetUseCase ──────────────────────────────────────────────────────

type RenameAssetInput struct {
	UserID      tree.UserID
	PageID      tree.PageID
	OldFilename tree.AssetName
	NewFilename tree.AssetName
}

type RenameAssetOutput struct {
	URL string
}

type RenameAssetUseCase struct {
	tree  *tree.TreeService
	asset *coreassets.AssetService
	log   *slog.Logger
}

func NewRenameAssetUseCase(t *tree.TreeService, a *coreassets.AssetService, log *slog.Logger) *RenameAssetUseCase {
	return &RenameAssetUseCase{tree: t, asset: a, log: log}
}

func (uc *RenameAssetUseCase) Execute(_ context.Context, in RenameAssetInput) (*RenameAssetOutput, error) {
	page, err := uc.tree.FindPageByID(in.PageID)
	if err != nil {
		if errors.Is(err, tree.ErrPageNotFound) {
			return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetPageNotFound, err, in.PageID.MetadataValue())
		}
		return nil, err
	}
	newPath, err := uc.asset.RenameAsset(page, in.OldFilename, in.NewFilename)
	if err != nil {
		return nil, err
	}
	return &RenameAssetOutput{URL: newPath}, nil
}

// ─── DeleteAssetUseCase ──────────────────────────────────────────────────────

type DeleteAssetInput struct {
	UserID   tree.UserID
	PageID   tree.PageID
	Filename tree.AssetName
}

type DeleteAssetUseCase struct {
	tree  *tree.TreeService
	asset *coreassets.AssetService
	log   *slog.Logger
}

func NewDeleteAssetUseCase(t *tree.TreeService, a *coreassets.AssetService, log *slog.Logger) *DeleteAssetUseCase {
	return &DeleteAssetUseCase{tree: t, asset: a, log: log}
}

func (uc *DeleteAssetUseCase) Execute(_ context.Context, in DeleteAssetInput) error {
	page, err := uc.tree.FindPageByID(in.PageID)
	if err != nil {
		if errors.Is(err, tree.ErrPageNotFound) {
			return sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetPageNotFound, err, in.PageID.MetadataValue())
		}
		return err
	}
	if err := uc.asset.DeleteAsset(page, in.Filename); err != nil {
		return err
	}
	return nil
}

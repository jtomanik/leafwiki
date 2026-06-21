package assets

import (
	"context"
	"errors"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	coreassets "github.com/perber/wiki/internal/core/assets"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

// ─── UploadAssetUseCase ──────────────────────────────────────────────────────

type UploadAssetInput struct {
	UserID   string
	PageID   string
	File     multipart.File
	Filename string
	MaxBytes int64
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
			return nil, sharederrors.NewLocalizedError("asset_page_not_found", "Page not found", "page %s not found", err, in.PageID)
		}
		return nil, err
	}
	url, err := uc.asset.SaveAssetForPage(page, in.File, in.Filename, in.MaxBytes)
	if err != nil {
		return nil, err
	}
	return &UploadAssetOutput{URL: url}, nil
}

// ─── ListAssetsUseCase ───────────────────────────────────────────────────────

type ListAssetsInput struct {
	PageID string
}

type ListAssetsOutput struct {
	Files []string
}

type ListAssetsUseCase struct {
	tree  *tree.TreeService
	asset *coreassets.AssetService
}

func NewListAssetsUseCase(t *tree.TreeService, a *coreassets.AssetService) *ListAssetsUseCase {
	return &ListAssetsUseCase{tree: t, asset: a}
}

func (uc *ListAssetsUseCase) Execute(_ context.Context, in ListAssetsInput) (*ListAssetsOutput, error) {
	page, err := uc.tree.FindPageByID(in.PageID)
	if err != nil {
		if errors.Is(err, tree.ErrPageNotFound) {
			return nil, sharederrors.NewLocalizedError("asset_page_not_found", "Page not found", "page %s not found", err, in.PageID)
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
	PageID   string
	Filename string
}

type GetAssetOutput struct {
	Filename string
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
			return nil, sharederrors.NewLocalizedError("asset_page_not_found", "Page not found", "page %s not found", err, in.PageID)
		}
		return nil, err
	}
	filename := strings.TrimSpace(in.Filename)
	content, err := uc.asset.ReadAssetForPage(page, filename)
	if err != nil {
		return nil, err
	}
	return &GetAssetOutput{
		Filename: filename,
		MIMEType: DetectAssetMIMEType(filename, content),
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
	UserID      string
	PageID      string
	OldFilename string
	NewFilename string
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
			return nil, sharederrors.NewLocalizedError("asset_page_not_found", "Page not found", "page %s not found", err, in.PageID)
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
	UserID   string
	PageID   string
	Filename string
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
			return sharederrors.NewLocalizedError("asset_page_not_found", "Page not found", "page %s not found", err, in.PageID)
		}
		return err
	}
	if err := uc.asset.DeleteAsset(page, in.Filename); err != nil {
		return err
	}
	return nil
}

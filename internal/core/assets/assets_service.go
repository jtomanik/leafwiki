package assets

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/perber/wiki/internal/core/shared"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

const DefaultMaxUploadSizeBytes shared.MaxBytes = 50 * 1024 * 1024

const (
	ErrCodeAssetUploadFailed     sharederrors.ErrorCode = "asset_upload_failed"
	ErrCodeAssetFileTooLarge     sharederrors.ErrorCode = "asset_file_too_large"
	ErrCodeAssetNotFound         sharederrors.ErrorCode = "asset_not_found"
	ErrCodeAssetReadFailed       sharederrors.ErrorCode = "asset_read_failed"
	ErrCodeAssetDeleteFailed     sharederrors.ErrorCode = "asset_delete_failed"
	ErrCodeAssetInvalidExtension sharederrors.ErrorCode = "asset_invalid_extension"
	ErrCodeAssetInvalidName      sharederrors.ErrorCode = "asset_invalid_name"
	ErrCodeAssetAlreadyExists    sharederrors.ErrorCode = "asset_already_exists"
	ErrCodeAssetRenameFailed     sharederrors.ErrorCode = "asset_rename_failed"
	ErrCodeAssetMissingName      sharederrors.ErrorCode = "asset_missing_name"
)

type AssetService struct {
	assetsDir string
	slugger   *tree.SlugService
	log       *slog.Logger

	mu sync.RWMutex
}

func assetPageDiskPath(assetsDir string, pageID tree.PageID) string {
	normalizedAssetsDir := filepath.FromSlash(strings.ReplaceAll(assetsDir, `\`, `/`))
	return filepath.Join(normalizedAssetsDir, pageID.MetadataValue())
}

func assetFileDiskPath(assetPath string, filename tree.AssetName) string {
	normalizedAssetPath := filepath.FromSlash(strings.ReplaceAll(assetPath, `\`, `/`))
	return filepath.Join(normalizedAssetPath, filename.Filename())
}

// validateFilename checks that a filename cannot escape its target directory.
// Rejects empty strings, path separators, and dot-only components like "." or "..".
func validateFilename(filename tree.AssetName) error {
	raw := filename.Filename()
	if raw == "" {
		return fmt.Errorf("filename must not be empty")
	}
	if strings.ContainsAny(raw, "/\\") {
		return fmt.Errorf("filename must not contain path separators")
	}
	if raw == "." || raw == ".." {
		return fmt.Errorf("filename must not be a dot component")
	}
	return nil
}

func NewAssetService(storageDir string, slugger *tree.SlugService) *AssetService {
	// Ensure the storage directory exists
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		panic(fmt.Sprintf("could not create storage directory: %v", err))
	}
	// Ensure the assets directory exists
	assetsDir := filepath.Join(storageDir, "assets")
	if err := os.MkdirAll(assetsDir, 0755); err != nil {
		panic(fmt.Sprintf("could not create assets directory: %v", err))
	}

	return &AssetService{
		assetsDir: assetsDir,
		slugger:   slugger,
		log:       slog.Default().With("component", "AssetService"),
	}
}

func (s *AssetService) GetAssetsDir() string {
	return s.assetsDir
}

func (s *AssetService) ensureAssetPagePathExists(page *tree.PageNode) (string, error) {
	pagePath := assetPageDiskPath(s.assetsDir, page.ID)
	// check if the page path exists
	if _, err := os.Stat(pagePath); os.IsNotExist(err) {
		// create the page path
		if err := os.MkdirAll(pagePath, 0755); err != nil {
			return "", fmt.Errorf("could not create page path: %w", err)
		}
	}

	return pagePath, nil
}

func (s *AssetService) getAssetPagePath(page *tree.PageNode) (string, error) {
	pagePath := assetPageDiskPath(s.assetsDir, page.ID)

	// check if the page path exists
	if _, err := os.Stat(pagePath); os.IsNotExist(err) {
		return "", fmt.Errorf("page path does not exist: %w", err)
	}

	return pagePath, nil
}

func (s *AssetService) buildPublicPath(page *tree.PageNode, filename tree.AssetName) string {
	return "/" + path.Join("assets", page.ID.MetadataValue(), filename.Filename())
}

// SaveAssetForPage saves a file under a page's slug-based path and returns its public URL.
func (s *AssetService) SaveAssetForPage(page *tree.PageNode, file multipart.File, originalFilename tree.AssetName, byteCap shared.MaxBytes) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	uploadPath, err := s.ensureAssetPagePathExists(page)
	if err != nil {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetUploadFailed, err)
	}

	// Read existing filenames
	entries, _ := os.ReadDir(uploadPath)
	existing := make([]string, 0, len(entries))
	for _, e := range entries {
		existing = append(existing, e.Name())
	}

	finalFilename := tree.AssetNameFromString(s.slugger.GenerateUniqueFilename(existing, originalFilename.Filename()))
	finalFilename, err = validateAssetFilename(finalFilename)
	if err != nil {
		return "", err
	}
	fullPath := assetFileDiskPath(uploadPath, finalFilename)

	if err := shared.WriteStreamAtomic(fullPath, file, byteCap); err != nil {
		if errors.Is(err, shared.ErrFileTooLarge) {
			return "", sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetFileTooLarge, err)
		}
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetUploadFailed, err)
	}

	// Return public path (served from /assets)
	return s.buildPublicPath(page, finalFilename), nil
}

// ListAssetsForPage returns the full paths of all assets for a given page
func (s *AssetService) ListAssetsForPage(page *tree.PageNode) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pagePath, err := s.getAssetPagePath(page)
	if err != nil {
		return []string{}, nil
	}

	files, err := os.ReadDir(pagePath)
	if err != nil && !os.IsNotExist(err) {
		return []string{}, nil
	}

	result := []string{}
	for _, f := range files {
		if !f.IsDir() {
			result = append(result, s.buildPublicPath(page, tree.AssetNameFromString(f.Name())))
		}
	}

	return result, nil
}

func (s *AssetService) ReadAssetForPage(page *tree.PageNode, filename tree.AssetName) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	filename, err := validateAssetFilename(filename)
	if err != nil {
		return nil, err
	}

	assetPath, err := s.getAssetPagePath(page)
	if err != nil {
		return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetNotFound, nil, filename.Filename())
	}

	data, err := os.ReadFile(assetFileDiskPath(assetPath, filename))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetNotFound, nil, filename.Filename())
		}
		return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetReadFailed, err, filename.Filename())
	}
	return data, nil
}

// DeleteAsset removes an asset file from disk
func (s *AssetService) DeleteAsset(page *tree.PageNode, filename tree.AssetName) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	filename, err := validateAssetFilename(filename)
	if err != nil {
		return err
	}

	assetPath, err := s.getAssetPagePath(page)
	if err != nil {
		return sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetNotFound, nil, filename.Filename())
	}

	fullPath := assetFileDiskPath(assetPath, filename)

	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			return sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetNotFound, nil, filename.Filename())
		}
		return sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetDeleteFailed, err, filename.Filename())
	}

	// Check if the directory is empty and remove it if so
	files, err := os.ReadDir(assetPath)
	if err == nil && len(files) == 0 {
		_ = os.Remove(assetPath) // we don't care if this fails
	}

	return nil
}

func (s *AssetService) DeleteAllAssetsForPage(page *tree.PageNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	assetDir, err := s.getAssetPagePath(page)
	if err != nil {
		// no assets dir -> nothing to delete
		return nil
	}
	if _, err := os.Stat(assetDir); err == nil {
		return os.RemoveAll(assetDir)
	}
	return nil
}

// RenameAsset renames an asset file for a given page.
func (s *AssetService) RenameAsset(page *tree.PageNode, oldFilename, newFilename tree.AssetName) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	oldFilename, err := validateAssetFilename(oldFilename)
	if err != nil {
		return "", err
	}
	newFilename, err = validateAssetFilename(newFilename)
	if err != nil {
		return "", err
	}

	assetPath, err := s.getAssetPagePath(page)
	if err != nil {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetNotFound, nil, oldFilename.Filename())
	}

	oldFullPath := assetFileDiskPath(assetPath, oldFilename)
	newFullPath := assetFileDiskPath(assetPath, newFilename)

	// Ensure that the new filename has the same extension as the old one
	oldExt := path.Ext(oldFilename.Filename())
	newExt := path.Ext(newFilename.Filename())
	if oldExt != newExt {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetInvalidExtension, nil, oldExt)
	}

	// Used for slug validation
	// The extension is not part of the slug, so we remove it
	newFilenameWithoutExt := newFilename.Filename()[:len(newFilename.Filename())-len(newExt)]
	// Ensure that the new asset is a valid filename (slug)
	if err := s.slugger.IsValidSlug(newFilenameWithoutExt); err != nil {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetInvalidName, nil, newFilename.Filename())
	}

	// Ensure that no file with the new name already exists
	if _, statErr := os.Stat(newFullPath); statErr == nil {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetAlreadyExists, nil, newFilename.Filename())
	} else if !os.IsNotExist(statErr) {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetRenameFailed, statErr, oldFilename.Filename())
	}

	if _, err := os.Stat(oldFullPath); os.IsNotExist(err) {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetNotFound, nil, oldFilename.Filename())
	}

	if err := os.Rename(oldFullPath, newFullPath); err != nil {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetRenameFailed, err, oldFilename.Filename())
	}

	return s.buildPublicPath(page, newFilename), nil
}

func validateAssetFilename(filename tree.AssetName) (tree.AssetName, error) {
	raw := strings.TrimSpace(filename.Filename())
	if raw == "" {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetMissingName, nil)
	}
	if raw == "." || raw == ".." {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetInvalidName, nil, raw)
	}
	if strings.ContainsAny(raw, `/\`) || raw != filepath.Base(raw) {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodeAssetInvalidName, nil, raw)
	}
	return tree.AssetNameFromString(raw), nil
}

func (s *AssetService) CopyAllAssets(sourcePage *tree.PageNode, targetPage *tree.PageNode) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	sourceAssetPath, err := s.getAssetPagePath(sourcePage)
	if err != nil {
		// No assets to copy
		return nil
	}

	targetAssetPath, err := s.ensureAssetPagePathExists(targetPage)
	if err != nil {
		return fmt.Errorf("could not create target asset path: %w", err)
	}

	entries, err := os.ReadDir(sourceAssetPath)
	if err != nil {
		return fmt.Errorf("could not read source asset directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue // skip directories
		}
		if err := s.copySingleAsset(sourceAssetPath, targetAssetPath, entry); err != nil {
			return fmt.Errorf("could not copy asset %s: %w", entry.Name(), err)
		}
	}

	return nil
}

func (s *AssetService) copySingleAsset(sourceAssetPath string, targetAssetPath string, entry os.DirEntry) error {
	filename := tree.AssetNameFromString(entry.Name())
	sourceFilePath := assetFileDiskPath(sourceAssetPath, filename)
	targetFilePath := assetFileDiskPath(targetAssetPath, filename)

	sourceFile, err := os.Open(sourceFilePath)
	if err != nil {
		return fmt.Errorf("could not open source asset file: %w", err)
	}
	defer logAssetFileClose(s.log, "failed to close source file", sourceFilePath, sourceFile)

	targetFile, err := os.Create(targetFilePath)
	if err != nil {
		return fmt.Errorf("could not create target asset file: %w", err)
	}
	defer logAssetFileClose(s.log, "failed to close target file", targetFilePath, targetFile)

	if _, err := io.Copy(targetFile, sourceFile); err != nil {
		return fmt.Errorf("could not copy asset file: %w", err)
	}

	return nil
}

func logAssetFileClose(log *slog.Logger, message string, filePath string, file interface{ Close() error }) {
	if err := file.Close(); err != nil {
		log.Warn(message, "file", filePath, "error", err)
	}
}

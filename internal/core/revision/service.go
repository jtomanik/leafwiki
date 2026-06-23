package revision

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/shared"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

// assetManifestEntry is the in-memory cache entry for a page's latest asset manifest hash.
type assetManifestEntry struct {
	hash string
}

const (
	errCodeRevisionPreviewContentUnavailable sharederrors.ErrorCode = "revision_preview_content_unavailable"
	errCodeRevisionPreviewAssetsUnavailable  sharederrors.ErrorCode = "revision_preview_assets_unavailable"
	errCodeRevisionPreviewAssetInvalidName   sharederrors.ErrorCode = "revision_preview_asset_invalid_name"
	errCodeRevisionPreviewAssetBlobMissing   sharederrors.ErrorCode = "revision_preview_asset_blob_unavailable"
	errCodeRevisionPreviewAssetNotFound      sharederrors.ErrorCode = "revision_preview_asset_not_found"
	errCodeRevisionRestoreInvalidPageID      sharederrors.ErrorCode = "revision_restore_invalid_page_id"
	errCodeRevisionRestoreInvalidRevision    sharederrors.ErrorCode = "revision_restore_invalid_revision"
	errCodeRevisionRestorePageNotFound       sharederrors.ErrorCode = "revision_restore_page_not_found"
	errCodeRevisionRestoreFailed             sharederrors.ErrorCode = "revision_restore_failed"
	errCodeRevisionRestoreRevisionNotFound   sharederrors.ErrorCode = "revision_restore_revision_not_found"
	errCodeRevisionRestoreContentMissing     sharederrors.ErrorCode = "revision_restore_content_missing"
	errCodeRevisionRestoreAssetsMissing      sharederrors.ErrorCode = "revision_restore_assets_missing"
	errCodeRevisionIntegrityMissingContent   sharederrors.ErrorCode = "missing_content_blob"
	errCodeRevisionIntegrityMissingManifest  sharederrors.ErrorCode = "missing_asset_manifest"
	errCodeRevisionIntegrityMissingAssetBlob sharederrors.ErrorCode = "missing_asset_blob"
	errCodeRevisionIntegrityHashMismatch     sharederrors.ErrorCode = "asset_blob_hash_mismatch"
	errCodeRevisionIntegritySizeMismatch     sharederrors.ErrorCode = "asset_blob_size_mismatch"
)

type Service struct {
	storageDir         string
	pages              *tree.TreeService
	store              *FSStore
	maxRevisions       int // 0 = unlimited
	log                *slog.Logger
	assetManifestCache sync.Map // pageID → assetManifestEntry
}

type ServiceOptions struct {
	MaxRevisions int // Maximum revisions to keep per page; 0 = unlimited
}

func NewService(storageDir string, pages *tree.TreeService, logger *slog.Logger, opts ...ServiceOptions) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	var maxRevisions int
	if len(opts) > 0 {
		maxRevisions = opts[0].MaxRevisions
	}

	store := NewFSStore(storageDir)
	return &Service{
		storageDir:   storageDir,
		pages:        pages,
		store:        store,
		maxRevisions: maxRevisions,
		log:          logger.With("component", "RevisionService"),
	}
}

// pruneAfterSave removes the oldest revisions beyond the configured limit.
// Errors are non-fatal and only logged — a prune failure must not fail the save.
func (s *Service) pruneAfterSave(pageID tree.PageID) {
	if s.maxRevisions <= 0 {
		return
	}
	if err := s.store.PruneRevisions(pageID, s.maxRevisions); err != nil {
		s.log.Warn("failed to prune old revisions", "pageID", pageID, "maxRevisions", s.maxRevisions, "error", err)
	}
}

// CapturePageState returns a full detached snapshot including current assets.
// This is the "expensive" path and is mainly used for asset changes and delete.
func (s *Service) CapturePageState(pageID tree.PageID) (*RevisionState, error) {
	return s.capturePageState(pageID, true)
}

// RecordContentUpdate records a content revision.
// Performance choice for V1:
//   - only content is re-hashed every time
//   - the latest asset manifest is reused if it already exists
//   - if this is the first revision for the page, assets are captured once
//
// Assumption: asset changes go through Upload/Rename/Delete hooks and call RecordAssetChange.
func (s *Service) RecordContentUpdate(pageID tree.PageID, authorID tree.UserID, summary string) (*Revision, bool, error) {
	page, err := s.pages.GetPage(pageID)
	if err != nil {
		return nil, false, err
	}
	return s.recordContentUpdateForPage(page, authorID, summary)
}

func (s *Service) RecordContentUpdates(pages []*tree.Page, authorID tree.UserID, summary string) []error {
	errs := make([]error, len(pages))
	if len(pages) == 0 {
		return errs
	}

	type batchItem struct {
		index int
		page  *tree.Page
	}

	grouped := make(map[tree.PageID][]batchItem)
	nilItems := make([]int, 0)
	for i, page := range pages {
		if page == nil {
			nilItems = append(nilItems, i)
			continue
		}
		grouped[page.ID] = append(grouped[page.ID], batchItem{index: i, page: page})
	}

	for _, i := range nilItems {
		errs[i] = fmt.Errorf("page is required")
	}

	parallelism := runtime.GOMAXPROCS(0)
	if parallelism < 1 {
		parallelism = 1
	}
	sem := make(chan struct{}, parallelism)

	var wg sync.WaitGroup
	wg.Add(len(grouped))
	for _, items := range grouped {
		go func(items []batchItem) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			for _, item := range items {
				if _, _, err := s.recordContentUpdateForPage(item.page, authorID, summary); err != nil {
					errs[item.index] = err
				}
			}
		}(items)
	}
	wg.Wait()

	return errs
}

// RecordAssetChange records a full snapshot when live assets changed.
// This method hashes the current assets and only writes a new revision when
// content or the asset manifest actually changed.
func (s *Service) RecordAssetChange(pageID tree.PageID, authorID tree.UserID, summary string) (*Revision, bool, error) {
	prev, err := s.store.GetLatestRevision(pageID)
	if err != nil {
		return nil, false, err
	}

	state, err := s.capturePageState(pageID, true)
	if err != nil {
		return nil, false, err
	}

	if prev != nil &&
		prev.ContentHash == state.ContentHash &&
		prev.AssetManifestHash == state.AssetManifestHash {
		return prev, false, nil
	}

	contentHash, err := s.store.SaveContentBlob([]byte(state.Content))
	if err != nil {
		return nil, false, err
	}
	if contentHash != state.ContentHash {
		return nil, false, fmt.Errorf("content hash mismatch: computed=%s saved=%s", state.ContentHash, contentHash)
	}

	if err := s.persistLiveAssets(pageID, state.Assets); err != nil {
		return nil, false, err
	}

	savedManifestHash, err := s.store.SaveAssetManifest(state.Assets)
	if err != nil {
		return nil, false, err
	}
	if savedManifestHash != state.AssetManifestHash {
		return nil, false, fmt.Errorf("asset manifest hash mismatch: computed=%s saved=%s", state.AssetManifestHash, savedManifestHash)
	}

	rev, err := s.newRevision(RevisionTypeAssetUpdate, state, authorID, summary, savedManifestHash)
	if err != nil {
		return nil, false, err
	}
	if err := s.store.SaveRevision(rev); err != nil {
		return nil, false, err
	}
	s.assetManifestCache.Store(rev.PageID, assetManifestEntry{hash: savedManifestHash})
	s.pruneAfterSave(rev.PageID)

	return rev, true, nil
}

func (s *Service) RecordStructureChange(pageID tree.PageID, authorID tree.UserID, summary string) (*Revision, bool, error) {
	prev, err := s.store.GetLatestRevision(pageID)
	if err != nil {
		return nil, false, err
	}

	state, err := s.capturePageState(pageID, false)
	if err != nil {
		return nil, false, err
	}

	assetManifestHash, err := s.resolveAssetManifestHash(pageID, prev)
	if err != nil {
		return nil, false, err
	}

	contentHash, err := s.store.SaveContentBlob([]byte(state.Content))
	if err != nil {
		return nil, false, err
	}
	if contentHash != state.ContentHash {
		return nil, false, fmt.Errorf("content hash mismatch: computed=%s saved=%s", state.ContentHash, contentHash)
	}

	rev, err := s.newRevision(RevisionTypeStructureUpdate, state, authorID, summary, assetManifestHash)
	if err != nil {
		return nil, false, err
	}
	if err := s.store.SaveRevision(rev); err != nil {
		return nil, false, err
	}
	s.pruneAfterSave(rev.PageID)

	return rev, true, nil
}

func (s *Service) resolveAssetManifestHash(pageID tree.PageID, prev *Revision) (string, error) {
	// Check in-memory cache first — avoids a full asset scan on every content save.
	// Use a stat to verify the file still exists without parsing its JSON content.
	if v, ok := s.assetManifestCache.Load(pageID); ok {
		entry := v.(assetManifestEntry)
		if s.store.AssetManifestExists(entry.hash) {
			return entry.hash, nil
		}
		s.assetManifestCache.Delete(pageID)
	}

	if prev != nil && prev.AssetManifestHash != "" {
		if _, err := s.store.LoadAssetManifest(prev.AssetManifestHash); err == nil {
			s.assetManifestCache.Store(pageID, assetManifestEntry{hash: prev.AssetManifestHash})
			return prev.AssetManifestHash, nil
		}
	}

	fullState, err := s.capturePageState(pageID, true)
	if err != nil {
		return "", err
	}
	if err := s.persistLiveAssets(pageID, fullState.Assets); err != nil {
		return "", err
	}
	savedManifestHash, err := s.store.SaveAssetManifest(fullState.Assets)
	if err != nil {
		return "", err
	}
	if savedManifestHash != fullState.AssetManifestHash {
		return "", fmt.Errorf("asset manifest hash mismatch: computed=%s saved=%s", fullState.AssetManifestHash, savedManifestHash)
	}
	s.assetManifestCache.Store(pageID, assetManifestEntry{hash: savedManifestHash})
	return savedManifestHash, nil
}

func (s *Service) ListRevisions(pageID tree.PageID) ([]*Revision, error) {
	return s.store.ListRevisions(pageID)
}

func (s *Service) ListRevisionsPage(pageID tree.PageID, cursor string, limit int) ([]*Revision, string, error) {
	return s.store.ListRevisionsPage(pageID, cursor, limit)
}

func (s *Service) GetLatestRevision(pageID tree.PageID) (*Revision, error) {
	return s.store.GetLatestRevision(pageID)
}

func (s *Service) GetRevisionSnapshot(pageID tree.PageID, revisionID RevisionID) (*RevisionSnapshot, error) {
	pageIDString := pageID.MetadataValue()
	revisionIDString := revisionID.CommitID()
	rev, err := s.store.GetRevision(pageID, revisionID)
	if err != nil {
		return nil, err
	}

	content, err := s.store.ReadContentBlob(rev.ContentHash)
	if err != nil {
		return nil, sharederrors.NewLocalizedError(
			errCodeRevisionPreviewContentUnavailable,
			"Revision content is unavailable",
			"revision content for page %s revision %s is unavailable",
			err,
			pageIDString,
			revisionIDString,
		)
	}

	assets, err := s.store.LoadAssetManifest(rev.AssetManifestHash)
	if err != nil {
		return nil, sharederrors.NewLocalizedError(
			errCodeRevisionPreviewAssetsUnavailable,
			"Revision assets are unavailable",
			"revision assets for page %s revision %s are unavailable",
			err,
			pageIDString,
			revisionIDString,
		)
	}

	return &RevisionSnapshot{
		Revision: rev,
		Content:  string(content),
		Assets:   cloneAndSortAssetRefs(assets),
	}, nil
}

func (s *Service) CompareRevisionSnapshots(pageID tree.PageID, baseRevisionID RevisionID, targetRevisionID RevisionID) (*RevisionComparison, error) {
	base, err := s.GetRevisionSnapshot(pageID, baseRevisionID)
	if err != nil {
		return nil, err
	}
	target, err := s.GetRevisionSnapshot(pageID, targetRevisionID)
	if err != nil {
		return nil, err
	}
	return &RevisionComparison{
		Base:           base,
		Target:         target,
		ContentChanged: base.Content != target.Content,
		AssetChanges:   compareRevisionAssets(base.Assets, target.Assets),
	}, nil
}

func (s *Service) GetRevisionAsset(pageID tree.PageID, revisionID RevisionID, assetName tree.AssetName) (*RevisionAssetContent, error) {
	pageIDString := pageID.MetadataValue()
	revisionIDString := revisionID.CommitID()
	assetNameString := strings.TrimSpace(strings.TrimPrefix(assetName.Filename(), "/"))
	if assetNameString == "" {
		return nil, sharederrors.NewLocalizedError(
			errCodeRevisionPreviewAssetInvalidName,
			"Revision asset name is invalid",
			"revision asset name for page %s revision %s is invalid",
			fmt.Errorf("asset name is required"),
			pageIDString,
			revisionIDString,
		)
	}

	rev, err := s.store.GetRevision(pageID, revisionID)
	if err != nil {
		return nil, err
	}

	assets, err := s.store.LoadAssetManifest(rev.AssetManifestHash)
	if err != nil {
		return nil, sharederrors.NewLocalizedError(
			errCodeRevisionPreviewAssetsUnavailable,
			"Revision assets are unavailable",
			"revision assets for page %s revision %s are unavailable",
			err,
			pageIDString,
			revisionIDString,
		)
	}

	for _, asset := range assets {
		if asset.Name != assetNameString {
			continue
		}

		blobPath := s.store.AssetBlobPath(asset.SHA256)
		if _, err := os.Stat(blobPath); err != nil {
			return nil, sharederrors.NewLocalizedError(
				errCodeRevisionPreviewAssetBlobMissing,
				"Revision asset is unavailable",
				"revision asset %s for page %s revision %s is unavailable",
				err,
				assetNameString,
				pageIDString,
				revisionIDString,
			)
		}

		return &RevisionAssetContent{
			Asset: asset,
			Path:  blobPath,
		}, nil
	}

	return nil, sharederrors.NewLocalizedError(
		errCodeRevisionPreviewAssetNotFound,
		"Revision asset not found",
		"revision asset %s for page %s revision %s not found",
		fmt.Errorf("asset %q not found in revision manifest", assetNameString),
		assetNameString,
		pageIDString,
		revisionIDString,
	)
}

func compareRevisionAssets(baseAssets, targetAssets []AssetRef) []RevisionAssetDelta {
	baseByName := make(map[string]AssetRef, len(baseAssets))
	for _, asset := range baseAssets {
		baseByName[asset.Name] = asset
	}
	targetByName := make(map[string]AssetRef, len(targetAssets))
	for _, asset := range targetAssets {
		targetByName[asset.Name] = asset
	}
	changes := make([]RevisionAssetDelta, 0)
	for name, baseAsset := range baseByName {
		targetAsset, ok := targetByName[name]
		if !ok {
			changes = append(changes, RevisionAssetDelta{Name: name, Status: "removed"})
			continue
		}
		if baseAsset.SHA256 != targetAsset.SHA256 || baseAsset.SizeBytes != targetAsset.SizeBytes {
			changes = append(changes, RevisionAssetDelta{Name: name, Status: "modified"})
		}
	}
	for name := range targetByName {
		if _, ok := baseByName[name]; !ok {
			changes = append(changes, RevisionAssetDelta{Name: name, Status: "added"})
		}
	}
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].Name < changes[j].Name })
	return changes
}

func (s *Service) DeletePageData(pageID tree.PageID) error {
	if pageID.MetadataValue() == "" {
		return nil
	}

	if err := s.store.DeletePageRevisions(pageID); err != nil {
		return err
	}
	s.assetManifestCache.Delete(pageID)

	return nil
}

func (s *Service) CheckRevisionIntegrity(pageID tree.PageID) ([]RevisionIntegrityIssue, error) {
	revisions, err := s.store.ListRevisions(pageID)
	if err != nil {
		return nil, err
	}

	issues := make([]RevisionIntegrityIssue, 0)
	for _, rev := range revisions {
		if rev == nil {
			continue
		}
		if strings.TrimSpace(rev.ContentHash) != "" {
			rc, err := s.store.OpenContentBlob(rev.ContentHash)
			if err != nil {
				issues = append(issues, RevisionIntegrityIssue{PageID: rev.PageID, RevisionID: rev.ID, Code: errCodeRevisionIntegrityMissingContent, Message: "Revision content blob is missing or unreadable", Path: s.store.contentBlobPath(rev.ContentHash)})
			} else {
				_ = rc.Close()
			}
		}
		refs, err := s.store.LoadAssetManifest(rev.AssetManifestHash)
		if err != nil {
			issues = append(issues, RevisionIntegrityIssue{PageID: rev.PageID, RevisionID: rev.ID, Code: errCodeRevisionIntegrityMissingManifest, Message: "Revision asset manifest is missing or unreadable", Path: s.store.assetManifestPath(rev.AssetManifestHash)})
			continue
		}
		for _, ref := range refs {
			blobPath := s.store.AssetBlobPath(ref.SHA256)
			f, err := s.store.OpenAssetBlob(ref.SHA256)
			if err != nil {
				issues = append(issues, RevisionIntegrityIssue{PageID: rev.PageID, RevisionID: rev.ID, Code: errCodeRevisionIntegrityMissingAssetBlob, Message: fmt.Sprintf("Revision asset blob for %s is missing or unreadable", ref.Name), Path: blobPath})
				continue
			}
			hasher := sha256.New()
			size, copyErr := io.Copy(hasher, f)
			_ = f.Close()
			if copyErr != nil {
				issues = append(issues, RevisionIntegrityIssue{PageID: rev.PageID, RevisionID: rev.ID, Code: errCodeRevisionIntegrityMissingAssetBlob, Message: fmt.Sprintf("Revision asset blob for %s is missing or unreadable", ref.Name), Path: blobPath})
				continue
			}
			if hex.EncodeToString(hasher.Sum(nil)) != ref.SHA256 {
				issues = append(issues, RevisionIntegrityIssue{PageID: rev.PageID, RevisionID: rev.ID, Code: errCodeRevisionIntegrityHashMismatch, Message: fmt.Sprintf("Revision asset blob for %s failed hash verification", ref.Name), Path: blobPath})
				continue
			}
			if size != ref.SizeBytes {
				issues = append(issues, RevisionIntegrityIssue{PageID: rev.PageID, RevisionID: rev.ID, Code: errCodeRevisionIntegritySizeMismatch, Message: fmt.Sprintf("Revision asset blob for %s failed size verification", ref.Name), Path: blobPath})
			}
		}
	}
	return issues, nil
}

func (s *Service) RestoreRevision(pageID tree.PageID, revisionID RevisionID, authorID tree.UserID) error {
	pageIDString := pageID.MetadataValue()
	revisionIDString := revisionID.CommitID()
	if pageIDString == "" {
		return sharederrors.NewLocalizedError(
			errCodeRevisionRestoreInvalidPageID,
			"Failed to restore page",
			"failed to restore page %s",
			nil,
			pageIDString,
		)
	}
	if revisionIDString == "" {
		return sharederrors.NewLocalizedError(
			errCodeRevisionRestoreInvalidRevision,
			"Restore revision is invalid",
			"restore revision %s for page %s is invalid",
			nil,
			revisionIDString,
			pageIDString,
		)
	}

	if _, err := s.pages.GetPage(pageID); err != nil {
		if errors.Is(err, tree.ErrPageNotFound) {
			return sharederrors.NewLocalizedError(
				errCodeRevisionRestorePageNotFound,
				"Page not found",
				"page %s not found",
				err,
				pageIDString,
			)
		}
		return sharederrors.NewLocalizedError(
			errCodeRevisionRestoreFailed,
			"Failed to restore page",
			"failed to restore page %s",
			err,
			pageIDString,
		)
	}

	rev, err := s.store.GetRevision(pageID, revisionID)
	if err != nil {
		if os.IsNotExist(err) {
			return sharederrors.NewLocalizedError(
				errCodeRevisionRestoreRevisionNotFound,
				"Restore revision not found",
				"restore revision %s for page %s not found",
				err,
				revisionIDString,
				pageIDString,
			)
		}
		return sharederrors.NewLocalizedError(
			errCodeRevisionRestoreFailed,
			"Failed to restore page",
			"failed to restore page %s",
			err,
			pageIDString,
		)
	}

	content, err := s.store.ReadContentBlob(rev.ContentHash)
	if err != nil {
		return sharederrors.NewLocalizedError(
			errCodeRevisionRestoreContentMissing,
			"Restore content is unavailable",
			"restore content for page %s is unavailable",
			err,
			pageIDString,
		)
	}

	assets, err := s.store.LoadAssetManifest(rev.AssetManifestHash)
	if err != nil {
		return sharederrors.NewLocalizedError(
			errCodeRevisionRestoreAssetsMissing,
			"Restore assets are unavailable",
			"restore assets for page %s are unavailable",
			err,
			pageIDString,
		)
	}

	beforeState, err := s.capturePageState(pageID, true)
	if err != nil {
		return sharederrors.NewLocalizedError(
			errCodeRevisionRestoreFailed,
			"Failed to restore page",
			"failed to restore page %s",
			err,
			pageIDString,
		)
	}

	restoredContent, restoreFromImport, err := buildRestoredRawContent(pageID, rev.Title, rev.PageMetadata, rev.ExtraFrontmatter, string(content))
	if err != nil {
		return sharederrors.NewLocalizedError(
			errCodeRevisionRestoreFailed,
			"Failed to restore page",
			"failed to restore page %s",
			err,
			pageIDString,
		)
	}
	if err := s.updateRestoredContent(authorID, pageID, rev.Title, beforeState.Slug, &restoredContent, restoreFromImport); err != nil {
		return sharederrors.NewLocalizedError(
			errCodeRevisionRestoreFailed,
			"Failed to restore page",
			"failed to restore page %s",
			err,
			pageIDString,
		)
	}

	if err := s.restoreAssets(pageID, assets); err != nil {
		restoreRollbackContent, rollbackFromImport, buildErr := buildRestoredRawContent(pageID, beforeState.Title, beforeState.PageMetadata, beforeState.ExtraFrontmatter, beforeState.Content)
		if buildErr != nil {
			s.log.Warn("failed to rebuild rollback content", "pageID", pageIDString, "error", buildErr)
			restoreRollbackContent = beforeState.Content
			rollbackFromImport = false
		}
		if rollbackErr := s.updateRestoredContent(authorID, pageID, beforeState.Title, beforeState.Slug, &restoreRollbackContent, rollbackFromImport); rollbackErr != nil {
			s.log.Warn("failed to rollback restored content", "pageID", pageIDString, "error", rollbackErr)
		}
		if rollbackErr := s.restoreAssets(pageID, beforeState.Assets); rollbackErr != nil {
			s.log.Warn("failed to rollback restored assets", "pageID", pageIDString, "error", rollbackErr)
		}
		return sharederrors.NewLocalizedError(
			errCodeRevisionRestoreFailed,
			"Failed to restore page",
			"failed to restore page %s",
			err,
			pageIDString,
		)
	}

	if err := s.recordRestoreRevision(pageID, authorID); err != nil {
		restoreRollbackContent, rollbackFromImport, buildErr := buildRestoredRawContent(pageID, beforeState.Title, beforeState.PageMetadata, beforeState.ExtraFrontmatter, beforeState.Content)
		if buildErr != nil {
			s.log.Warn("failed to rebuild rollback content", "pageID", pageIDString, "error", buildErr)
			restoreRollbackContent = beforeState.Content
			rollbackFromImport = false
		}
		if rollbackErr := s.updateRestoredContent(authorID, pageID, beforeState.Title, beforeState.Slug, &restoreRollbackContent, rollbackFromImport); rollbackErr != nil {
			s.log.Warn("failed to rollback restored content", "pageID", pageIDString, "error", rollbackErr)
		}
		if rollbackErr := s.restoreAssets(pageID, beforeState.Assets); rollbackErr != nil {
			s.log.Warn("failed to rollback restored assets", "pageID", pageIDString, "error", rollbackErr)
		}
		return sharederrors.NewLocalizedError(
			errCodeRevisionRestoreFailed,
			"Failed to restore page",
			"failed to restore page %s",
			err,
			pageIDString,
		)
	}

	return nil
}

func (s *Service) updateRestoredContent(authorID tree.UserID, pageID tree.PageID, title string, slug tree.Slug, content *string, replaceMetadata bool) error {
	if replaceMetadata {
		return s.pages.UpdateNodeReplacingMetadataUncheckedVersion(authorID, pageID, title, slug, content)
	}
	return s.pages.UpdateNodeUncheckedVersion(authorID, pageID, title, slug, content, false)
}

func (s *Service) capturePageState(pageID tree.PageID, withAssets bool) (*RevisionState, error) {
	page, err := s.pages.GetPage(pageID)
	if err != nil {
		return nil, err
	}

	state := s.revisionStateFromPage(page)
	if err := s.enrichStateWithExtraFrontmatter(page.ID, state); err != nil {
		return nil, err
	}

	if !withAssets {
		return state, nil
	}

	assets, err := s.scanLiveAssets(pageID)
	if err != nil {
		return nil, err
	}

	state.Assets = assets
	hash, err := computeAssetManifestHash(assets)
	if err != nil {
		return nil, err
	}
	state.AssetManifestHash = hash

	return state, nil
}

func (s *Service) revisionStateFromPage(page *tree.Page) *RevisionState {
	var parentID tree.PageID
	if page.Parent != nil && page.Parent.ID != tree.RootPageID {
		parentID = page.Parent.ID
	}

	return &RevisionState{
		PageID:        page.ID,
		ParentID:      parentID,
		Title:         page.Title,
		Slug:          page.Slug,
		Kind:          string(page.Kind),
		Path:          page.CalculatePath(),
		Content:       page.Content,
		ContentHash:   sha256HexBytes([]byte(page.Content)),
		PageCreatedAt: page.Metadata.CreatedAt.UTC(),
		PageUpdatedAt: page.Metadata.UpdatedAt.UTC(),
		CreatorID:     page.Metadata.CreatorID.MetadataValue(),
		LastAuthorID:  page.Metadata.LastAuthorID.MetadataValue(),
		CapturedAt:    time.Now().UTC(),
	}
}

func (s *Service) recordContentUpdateForPage(page *tree.Page, authorID tree.UserID, summary string) (*Revision, bool, error) {
	prev, err := s.store.GetLatestRevision(page.ID)
	if err != nil {
		return nil, false, err
	}

	state := s.revisionStateFromPage(page)
	if err := s.enrichStateWithExtraFrontmatter(page.ID, state); err != nil {
		return nil, false, err
	}

	if prev != nil && prev.ContentHash == state.ContentHash && revisionStoredMetadataHash(prev) == state.PageMetadataHash {
		return prev, false, nil
	}

	assetManifestHash, err := s.resolveAssetManifestHash(page.ID, prev)
	if err != nil {
		return nil, false, err
	}

	contentHash, err := s.store.SaveContentBlob([]byte(state.Content))
	if err != nil {
		return nil, false, err
	}
	if contentHash != state.ContentHash {
		return nil, false, fmt.Errorf("content hash mismatch: computed=%s saved=%s", state.ContentHash, contentHash)
	}

	rev, err := s.newRevision(RevisionTypeContentUpdate, state, authorID, summary, assetManifestHash)
	if err != nil {
		return nil, false, err
	}
	if err := s.store.SaveRevision(rev); err != nil {
		return nil, false, err
	}
	s.pruneAfterSave(rev.PageID)

	return rev, true, nil
}

func (s *Service) newRevision(t RevisionType, state *RevisionState, authorID tree.UserID, summary, assetManifestHash string) (*Revision, error) {
	revisionID, err := shared.GenerateUniqueID()
	if err != nil {
		return nil, fmt.Errorf("generate revision id: %w", err)
	}

	return &Revision{
		ID:                   NewRevisionIDUnchecked(revisionID),
		PageID:               state.PageID,
		ParentID:             state.ParentID,
		Type:                 t,
		AuthorID:             authorID.MetadataValue(),
		CreatedAt:            time.Now().UTC(),
		Title:                state.Title,
		Slug:                 state.Slug,
		Kind:                 state.Kind,
		Path:                 state.Path,
		ContentHash:          state.ContentHash,
		ExtraFrontmatter:     state.ExtraFrontmatter,
		ExtraFrontmatterHash: state.ExtraFrontmatterHash,
		PageMetadata:         state.PageMetadata,
		PageMetadataHash:     state.PageMetadataHash,
		AssetManifestHash:    assetManifestHash,
		PageCreatedAt:        state.PageCreatedAt.UTC(),
		PageUpdatedAt:        state.PageUpdatedAt.UTC(),
		CreatorID:            strings.TrimSpace(state.CreatorID),
		LastAuthorID:         strings.TrimSpace(state.LastAuthorID),
		Summary:              summary,
	}, nil
}

func (s *Service) enrichStateWithExtraFrontmatter(pageID tree.PageID, state *RevisionState) error {
	if state == nil {
		return fmt.Errorf("revision state is required")
	}

	raw, err := s.pages.ReadPageRaw(pageID)
	if err != nil {
		return err
	}

	doc, _, err := markdown.ParsePageDocument(raw)
	if err != nil {
		return err
	}
	metadata := revisionPageMetadata(doc.Metadata)
	if metadata == nil {
		state.PageMetadata = nil
		state.PageMetadataHash = ""
		state.ExtraFrontmatter = nil
		state.ExtraFrontmatterHash = ""
		return nil
	}
	metadataHash, err := hashPageMetadata(metadata)
	if err != nil {
		return err
	}
	state.PageMetadata = metadata
	state.PageMetadataHash = metadataHash
	state.ExtraFrontmatter = nil
	state.ExtraFrontmatterHash = ""
	return nil
}

func revisionStoredMetadataHash(rev *Revision) string {
	if rev == nil {
		return ""
	}
	if strings.TrimSpace(rev.PageMetadataHash) != "" {
		return rev.PageMetadataHash
	}
	return rev.ExtraFrontmatterHash
}

func revisionPageMetadata(meta markdown.PageMetadata) *markdown.PageMetadata {
	if meta.Version == 0 {
		return nil
	}
	snapshot := markdown.PageMetadata{
		Version: 1,
		Tags:    append([]string{}, meta.Tags...),
		Fields:  cloneMetadataMap(meta.Fields),
		Extra:   cloneMetadataMap(meta.Extra),
	}
	if len(snapshot.Fields) == 0 {
		snapshot.Fields = nil
	}
	if len(snapshot.Extra) == 0 {
		snapshot.Extra = nil
	}
	return &snapshot
}

func hashPageMetadata(meta *markdown.PageMetadata) (string, error) {
	if meta == nil {
		return "", nil
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshal page metadata: %w", err)
	}
	return sha256HexBytes(raw), nil
}

func hashExtraFrontmatter(extra map[string]interface{}) (string, error) {
	if len(extra) == 0 {
		return "", nil
	}

	raw, err := json.Marshal(extra)
	if err != nil {
		return "", fmt.Errorf("marshal compatibility metadata extras: %w", err)
	}
	return sha256HexBytes(raw), nil
}

func buildRestoredRawContent(pageID tree.PageID, title string, metadata *markdown.PageMetadata, extra map[string]interface{}, body string) (string, bool, error) {
	if metadata != nil {
		meta := clonePageMetadata(*metadata)
		meta.Version = 1
		meta.Page = markdown.PageMetadataPage{
			ID:    pageID.MetadataValue(),
			Title: strings.TrimSpace(title),
		}
		raw, err := markdown.RenderPageDocument(markdown.PageDocument{
			Body:     body,
			Metadata: meta,
		})
		if err != nil {
			return "", false, err
		}
		return raw, true, nil
	}

	if len(extra) == 0 {
		return body, false, nil
	}

	raw, err := markdown.BuildMarkdownWithMetadata(markdown.Frontmatter{
		LeafWikiID:    pageID.MetadataValue(),
		LeafWikiTitle: strings.TrimSpace(title),
		ExtraFields:   extra,
	}, body)
	if err != nil {
		return "", false, err
	}

	return raw, true, nil
}

func clonePageMetadata(meta markdown.PageMetadata) markdown.PageMetadata {
	cloned := meta
	cloned.Tags = append([]string{}, meta.Tags...)
	cloned.Fields = cloneMetadataMap(meta.Fields)
	if len(cloned.Fields) == 0 {
		cloned.Fields = nil
	}
	cloned.Extra = cloneMetadataMap(meta.Extra)
	if len(cloned.Extra) == 0 {
		cloned.Extra = nil
	}
	return cloned
}

func cloneMetadataMap(values map[string]interface{}) map[string]interface{} {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}

func (s *Service) persistLiveAssets(pageID tree.PageID, refs []AssetRef) error {
	if len(refs) == 0 {
		return nil
	}

	for _, ref := range refs {
		srcPath := filepath.Join(s.liveAssetDir(pageID), ref.Name)
		hash, size, err := s.store.SaveAssetBlobFromPath(srcPath)
		if err != nil {
			return err
		}
		if hash != ref.SHA256 {
			return fmt.Errorf("asset hash mismatch for %s: computed=%s saved=%s", ref.Name, ref.SHA256, hash)
		}
		if size != ref.SizeBytes {
			return fmt.Errorf("asset size mismatch for %s: computed=%d saved=%d", ref.Name, ref.SizeBytes, size)
		}
	}
	return nil
}

func (s *Service) scanLiveAssets(pageID tree.PageID) ([]AssetRef, error) {
	dir := s.liveAssetDir(pageID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []AssetRef{}, nil
		}
		return nil, fmt.Errorf("read live asset dir %s: %w", dir, err)
	}

	refs := make([]AssetRef, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		absPath := filepath.Join(dir, name)

		ref, err := buildAssetRef(absPath, name)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}

	sort.SliceStable(refs, func(i, j int) bool {
		if refs[i].Name == refs[j].Name {
			return refs[i].SHA256 < refs[j].SHA256
		}
		return refs[i].Name < refs[j].Name
	})

	return refs, nil
}

// Assumption for V1:
// live assets are stored under <storageDir>/assets/<pageID>/...
// If your AssetService uses a different on-disk layout, only change this method.
func (s *Service) liveAssetDir(pageID tree.PageID) string {
	return filepath.Join(s.storageDir, "assets", pageID.MetadataValue())
}

func buildAssetRef(absPath, name string) (AssetRef, error) {
	file, err := os.Open(absPath)
	if err != nil {
		return AssetRef{}, fmt.Errorf("open asset %s: %w", absPath, err)
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return AssetRef{}, fmt.Errorf("hash asset %s: %w", absPath, err)
	}

	mimeType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	return AssetRef{
		Name:      name,
		SHA256:    hex.EncodeToString(hasher.Sum(nil)),
		SizeBytes: size,
		MIMEType:  mimeType,
	}, nil
}

func computeAssetManifestHash(items []AssetRef) (string, error) {
	canonical := cloneAndSortAssetRefs(items)

	raw, err := json.Marshal(assetManifest{Items: canonical})
	if err != nil {
		return "", fmt.Errorf("marshal asset manifest for hash: %w", err)
	}

	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Service) restoreAssets(pageID tree.PageID, refs []AssetRef) error {
	dir := s.liveAssetDir(pageID)
	if err := os.RemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reset live asset dir: %w", err)
	}
	if len(refs) == 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("ensure live asset dir: %w", err)
	}

	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		name := strings.TrimSpace(ref.Name)
		if name == "" || filepath.Base(name) != name || strings.Contains(name, string(os.PathSeparator)) {
			return fmt.Errorf("invalid asset name: %s", ref.Name)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate asset name in manifest: %s", name)
		}
		seen[name] = struct{}{}

		if err := s.store.CopyAssetBlobToPath(ref.SHA256, ref.SizeBytes, filepath.Join(dir, name)); err != nil {
			return fmt.Errorf("restore asset %s: %w", name, err)
		}
	}

	return nil
}

func (s *Service) recordRestoreRevision(pageID tree.PageID, authorID tree.UserID) error {
	state, err := s.capturePageState(pageID, true)
	if err != nil {
		return err
	}

	contentHash, err := s.store.SaveContentBlob([]byte(state.Content))
	if err != nil {
		return err
	}
	if contentHash != state.ContentHash {
		return fmt.Errorf("content hash mismatch: computed=%s saved=%s", state.ContentHash, contentHash)
	}

	if err := s.persistLiveAssets(pageID, state.Assets); err != nil {
		return err
	}

	savedManifestHash, err := s.store.SaveAssetManifest(state.Assets)
	if err != nil {
		return err
	}
	if savedManifestHash != state.AssetManifestHash {
		return fmt.Errorf("asset manifest hash mismatch: computed=%s saved=%s", state.AssetManifestHash, savedManifestHash)
	}

	rev, err := s.newRevision(RevisionTypeRestore, state, authorID, "page restored", savedManifestHash)
	if err != nil {
		return err
	}
	return s.store.SaveRevision(rev)
}

package revision

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"

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

var (
	ErrAssetHashRequired         = errors.New("asset hash is required")
	ErrRevisionCreatedAtRequired = errors.New("created_at is required")
	ErrContentHashMismatch       = errors.New("content hash mismatch")
	ErrAssetManifestHashMismatch = errors.New("asset manifest hash mismatch")
	ErrAssetBlobHashMismatch     = errors.New("asset blob hash mismatch")
	ErrAssetBlobSizeMismatch     = errors.New("asset blob size mismatch")
	ErrInvalidAssetName          = errors.New("invalid asset name")
	ErrDuplicateAssetName        = errors.New("duplicate asset name")
	ErrRevisionValidation        = errors.New("revision validation failed")
	ErrRevisionStateRequired     = errors.New("revision state is required")
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
	page, err := revisionPagesGetPage(s.pages, pageID)
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
		errs[i] = fmt.Errorf("%w: page is required", ErrRevisionValidation)
	}

	parallelism := revisionGOMAXPROCS(0)
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
	prev, err := revisionStoreGetLatestRevision(s.store, pageID)
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

	contentHash, err := revisionStoreSaveContentBlob(s.store, []byte(state.Content))
	if err != nil {
		return nil, false, err
	}
	if contentHash != state.ContentHash {
		return nil, false, fmt.Errorf("%w: computed=%s saved=%s", ErrContentHashMismatch, state.ContentHash, contentHash)
	}

	if err := s.persistLiveAssets(pageID, state.Assets); err != nil {
		return nil, false, err
	}

	savedManifestHash, err := revisionStoreSaveAssetManifest(s.store, state.Assets)
	if err != nil {
		return nil, false, err
	}
	if savedManifestHash != state.AssetManifestHash {
		return nil, false, fmt.Errorf("%w: computed=%s saved=%s", ErrAssetManifestHashMismatch, state.AssetManifestHash, savedManifestHash)
	}

	rev, err := s.newRevision(RevisionTypeAssetUpdate, state, authorID, summary, savedManifestHash)
	if err != nil {
		return nil, false, err
	}
	if err := revisionStoreSaveRevision(s.store, rev); err != nil {
		return nil, false, err
	}
	s.assetManifestCache.Store(rev.PageID, assetManifestEntry{hash: savedManifestHash})
	s.pruneAfterSave(rev.PageID)

	return rev, true, nil
}

func (s *Service) RecordStructureChange(pageID tree.PageID, authorID tree.UserID, summary string) (*Revision, bool, error) {
	prev, err := revisionStoreGetLatestRevision(s.store, pageID)
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

	contentHash, err := revisionStoreSaveContentBlob(s.store, []byte(state.Content))
	if err != nil {
		return nil, false, err
	}
	if contentHash != state.ContentHash {
		return nil, false, fmt.Errorf("%w: computed=%s saved=%s", ErrContentHashMismatch, state.ContentHash, contentHash)
	}

	rev, err := s.newRevision(RevisionTypeStructureUpdate, state, authorID, summary, assetManifestHash)
	if err != nil {
		return nil, false, err
	}
	if err := revisionStoreSaveRevision(s.store, rev); err != nil {
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
		if revisionStoreAssetManifestExists(s.store, entry.hash) {
			return entry.hash, nil
		}
		s.assetManifestCache.Delete(pageID)
	}

	if prev != nil && prev.AssetManifestHash != "" {
		if _, err := revisionStoreLoadAssetManifest(s.store, prev.AssetManifestHash); err == nil {
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
	savedManifestHash, err := revisionStoreSaveAssetManifest(s.store, fullState.Assets)
	if err != nil {
		return "", err
	}
	if savedManifestHash != fullState.AssetManifestHash {
		return "", fmt.Errorf("%w: computed=%s saved=%s", ErrAssetManifestHashMismatch, fullState.AssetManifestHash, savedManifestHash)
	}
	s.assetManifestCache.Store(pageID, assetManifestEntry{hash: savedManifestHash})
	return savedManifestHash, nil
}

func (s *Service) ListRevisions(pageID tree.PageID) ([]*Revision, error) {
	return revisionStoreListRevisions(s.store, pageID)
}

func (s *Service) ListRevisionsPage(pageID tree.PageID, cursor string, pageSize RevisionListLimit) ([]*Revision, string, error) {
	return s.store.ListRevisionsPage(pageID, cursor, pageSize)
}

func (s *Service) GetLatestRevision(pageID tree.PageID) (*Revision, error) {
	return revisionStoreGetLatestRevision(s.store, pageID)
}

func (s *Service) GetRevisionSnapshot(pageID tree.PageID, revisionID RevisionID) (*RevisionSnapshot, error) {
	pageIDString := pageID.MetadataValue()
	revisionIDString := revisionID.CommitID()
	rev, err := revisionStoreGetRevision(s.store, pageID, revisionID)
	if err != nil {
		return nil, err
	}

	content, err := revisionStoreReadContentBlob(s.store, rev.ContentHash)
	if err != nil {
		return nil, sharederrors.NewLocalizedErrorFromCode(errCodeRevisionPreviewContentUnavailable, err, pageIDString, revisionIDString)
	}

	assets, err := revisionStoreLoadAssetManifest(s.store, rev.AssetManifestHash)
	if err != nil {
		return nil, sharederrors.NewLocalizedErrorFromCode(errCodeRevisionPreviewAssetsUnavailable, err, pageIDString, revisionIDString)
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
		return nil, sharederrors.NewLocalizedErrorFromCode(errCodeRevisionPreviewAssetInvalidName, fmt.Errorf("asset name is required"), pageIDString, revisionIDString)
	}

	rev, err := revisionStoreGetRevision(s.store, pageID, revisionID)
	if err != nil {
		return nil, err
	}

	assets, err := revisionStoreLoadAssetManifest(s.store, rev.AssetManifestHash)
	if err != nil {
		return nil, sharederrors.NewLocalizedErrorFromCode(errCodeRevisionPreviewAssetsUnavailable, err, pageIDString, revisionIDString)
	}

	for _, asset := range assets {
		if asset.Name != assetNameString {
			continue
		}

		blobPath := s.store.AssetBlobPath(asset.SHA256)
		if _, err := revisionStat(blobPath); err != nil {
			return nil, sharederrors.NewLocalizedErrorFromCode(errCodeRevisionPreviewAssetBlobMissing, err, assetNameString, pageIDString, revisionIDString)
		}

		return &RevisionAssetContent{
			Asset: asset,
			Path:  blobPath,
		}, nil
	}

	return nil, sharederrors.NewLocalizedErrorFromCode(errCodeRevisionPreviewAssetNotFound, fmt.Errorf("asset %q not found in revision manifest", assetNameString), assetNameString, pageIDString, revisionIDString)
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

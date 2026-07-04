package revision

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"os"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
)

func (s *Service) DeletePageData(pageID tree.PageID) error {
	if pageID.MetadataValue() == "" {
		return nil
	}

	if err := revisionStoreDeletePageRevisions(s.store, pageID); err != nil {
		return err
	}
	s.assetManifestCache.Delete(pageID)

	return nil
}

func (s *Service) CheckRevisionIntegrity(pageID tree.PageID) ([]RevisionIntegrityIssue, error) {
	revisions, err := revisionStoreListRevisions(s.store, pageID)
	if err != nil {
		return nil, err
	}

	issues := make([]RevisionIntegrityIssue, 0)
	for _, rev := range revisions {
		if rev == nil {
			continue
		}
		if strings.TrimSpace(rev.ContentHash) != "" {
			rc, err := revisionStoreOpenContentBlob(s.store, rev.ContentHash)
			if err != nil {
				issues = append(issues, RevisionIntegrityIssue{PageID: rev.PageID, RevisionID: rev.ID, Code: errCodeRevisionIntegrityMissingContent, MessageID: sharederrors.MessageIDForCode(errCodeRevisionIntegrityMissingContent), Message: "Revision content blob is missing or unreadable", Path: s.store.contentBlobPath(rev.ContentHash)})
			} else {
				_ = rc.Close()
			}
		}
		refs, err := revisionStoreLoadAssetManifest(s.store, rev.AssetManifestHash)
		if err != nil {
			issues = append(issues, RevisionIntegrityIssue{PageID: rev.PageID, RevisionID: rev.ID, Code: errCodeRevisionIntegrityMissingManifest, MessageID: sharederrors.MessageIDForCode(errCodeRevisionIntegrityMissingManifest), Message: "Revision asset manifest is missing or unreadable", Path: s.store.assetManifestPath(rev.AssetManifestHash)})
			continue
		}
		for _, ref := range refs {
			blobPath := s.store.AssetBlobPath(ref.SHA256)
			f, err := revisionStoreOpenAssetBlob(s.store, ref.SHA256)
			if err != nil {
				issues = append(issues, RevisionIntegrityIssue{PageID: rev.PageID, RevisionID: rev.ID, Code: errCodeRevisionIntegrityMissingAssetBlob, MessageID: sharederrors.MessageIDForCode(errCodeRevisionIntegrityMissingAssetBlob), Message: fmt.Sprintf("Revision asset blob for %s is missing or unreadable", ref.Name), Path: blobPath})
				continue
			}
			hasher := sha256.New()
			size, copyErr := revisionCopy(hasher, f)
			_ = revisionFileClose(f)
			if copyErr != nil {
				issues = append(issues, RevisionIntegrityIssue{PageID: rev.PageID, RevisionID: rev.ID, Code: errCodeRevisionIntegrityMissingAssetBlob, MessageID: sharederrors.MessageIDForCode(errCodeRevisionIntegrityMissingAssetBlob), Message: fmt.Sprintf("Revision asset blob for %s is missing or unreadable", ref.Name), Path: blobPath})
				continue
			}
			if hex.EncodeToString(hasher.Sum(nil)) != ref.SHA256 {
				issues = append(issues, RevisionIntegrityIssue{PageID: rev.PageID, RevisionID: rev.ID, Code: errCodeRevisionIntegrityHashMismatch, MessageID: sharederrors.MessageIDForCode(errCodeRevisionIntegrityHashMismatch), Message: fmt.Sprintf("Revision asset blob for %s failed hash verification", ref.Name), Path: blobPath})
				continue
			}
			if size != ref.SizeBytes {
				issues = append(issues, RevisionIntegrityIssue{PageID: rev.PageID, RevisionID: rev.ID, Code: errCodeRevisionIntegritySizeMismatch, MessageID: sharederrors.MessageIDForCode(errCodeRevisionIntegritySizeMismatch), Message: fmt.Sprintf("Revision asset blob for %s failed size verification", ref.Name), Path: blobPath})
			}
		}
	}
	return issues, nil
}

func (s *Service) RestoreRevision(pageID tree.PageID, revisionID RevisionID, authorID tree.UserID) error {
	pageIDString := pageID.MetadataValue()
	revisionIDString := revisionID.CommitID()
	if pageIDString == "" {
		return sharederrors.NewLocalizedErrorFromCode(errCodeRevisionRestoreInvalidPageID, nil, pageIDString)
	}
	if revisionIDString == "" {
		return sharederrors.NewLocalizedErrorFromCode(errCodeRevisionRestoreInvalidRevision, nil, revisionIDString, pageIDString)
	}

	if _, err := s.pages.GetPage(pageID); err != nil {
		if errors.Is(err, tree.ErrPageNotFound) {
			return sharederrors.NewLocalizedErrorFromCode(errCodeRevisionRestorePageNotFound, err, pageIDString)
		}
		return sharederrors.NewLocalizedErrorFromCode(errCodeRevisionRestoreFailed, err, pageIDString)
	}

	rev, err := revisionStoreGetRevision(s.store, pageID, revisionID)
	if err != nil {
		if os.IsNotExist(err) {
			return sharederrors.NewLocalizedErrorFromCode(errCodeRevisionRestoreRevisionNotFound, err, revisionIDString, pageIDString)
		}
		return sharederrors.NewLocalizedErrorFromCode(errCodeRevisionRestoreFailed, err, pageIDString)
	}

	content, err := revisionStoreReadContentBlob(s.store, rev.ContentHash)
	if err != nil {
		return sharederrors.NewLocalizedErrorFromCode(errCodeRevisionRestoreContentMissing, err, pageIDString)
	}

	assets, err := revisionStoreLoadAssetManifest(s.store, rev.AssetManifestHash)
	if err != nil {
		return sharederrors.NewLocalizedErrorFromCode(errCodeRevisionRestoreAssetsMissing, err, pageIDString)
	}

	beforeState, err := s.capturePageState(pageID, true)
	if err != nil {
		return sharederrors.NewLocalizedErrorFromCode(errCodeRevisionRestoreFailed, err, pageIDString)
	}

	restoredContent, restoreFromImport, err := revisionBuildRestoredRawContent(pageID, rev.Title, rev.PageMetadata, rev.ExtraFrontmatter, string(content))
	if err != nil {
		return sharederrors.NewLocalizedErrorFromCode(errCodeRevisionRestoreFailed, err, pageIDString)
	}
	if err := revisionUpdateRestoredContent(s, authorID, pageID, rev.Title, beforeState.Slug, &restoredContent, restoreFromImport); err != nil {
		return sharederrors.NewLocalizedErrorFromCode(errCodeRevisionRestoreFailed, err, pageIDString)
	}

	if err := revisionRestoreAssets(s, pageID, assets); err != nil {
		restoreRollbackContent, rollbackFromImport, buildErr := revisionBuildRestoredRawContent(pageID, beforeState.Title, beforeState.PageMetadata, beforeState.ExtraFrontmatter, beforeState.Content)
		if buildErr != nil {
			s.log.Warn("failed to rebuild rollback content", "pageID", pageIDString, "error", buildErr)
			restoreRollbackContent = beforeState.Content
			rollbackFromImport = false
		}
		if rollbackErr := revisionUpdateRestoredContent(s, authorID, pageID, beforeState.Title, beforeState.Slug, &restoreRollbackContent, rollbackFromImport); rollbackErr != nil {
			s.log.Warn("failed to rollback restored content", "pageID", pageIDString, "error", rollbackErr)
		}
		if rollbackErr := revisionRestoreAssets(s, pageID, beforeState.Assets); rollbackErr != nil {
			s.log.Warn("failed to rollback restored assets", "pageID", pageIDString, "error", rollbackErr)
		}
		return sharederrors.NewLocalizedErrorFromCode(errCodeRevisionRestoreFailed, err, pageIDString)
	}

	if err := revisionRecordRestoreRevision(s, pageID, authorID); err != nil {
		restoreRollbackContent, rollbackFromImport, buildErr := revisionBuildRestoredRawContent(pageID, beforeState.Title, beforeState.PageMetadata, beforeState.ExtraFrontmatter, beforeState.Content)
		if buildErr != nil {
			s.log.Warn("failed to rebuild rollback content", "pageID", pageIDString, "error", buildErr)
			restoreRollbackContent = beforeState.Content
			rollbackFromImport = false
		}
		if rollbackErr := revisionUpdateRestoredContent(s, authorID, pageID, beforeState.Title, beforeState.Slug, &restoreRollbackContent, rollbackFromImport); rollbackErr != nil {
			s.log.Warn("failed to rollback restored content", "pageID", pageIDString, "error", rollbackErr)
		}
		if rollbackErr := revisionRestoreAssets(s, pageID, beforeState.Assets); rollbackErr != nil {
			s.log.Warn("failed to rollback restored assets", "pageID", pageIDString, "error", rollbackErr)
		}
		return sharederrors.NewLocalizedErrorFromCode(errCodeRevisionRestoreFailed, err, pageIDString)
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
		Kind:          page.Kind,
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
	prev, err := revisionStoreGetLatestRevision(s.store, page.ID)
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

	contentHash, err := revisionStoreSaveContentBlob(s.store, []byte(state.Content))
	if err != nil {
		return nil, false, err
	}
	if contentHash != state.ContentHash {
		return nil, false, fmt.Errorf("%w: computed=%s saved=%s", ErrContentHashMismatch, state.ContentHash, contentHash)
	}

	rev, err := s.newRevision(RevisionTypeContentUpdate, state, authorID, summary, assetManifestHash)
	if err != nil {
		return nil, false, err
	}
	if err := revisionStoreSaveRevision(s.store, rev); err != nil {
		return nil, false, err
	}
	s.pruneAfterSave(rev.PageID)

	return rev, true, nil
}

func (s *Service) newRevision(t RevisionType, state *RevisionState, authorID tree.UserID, summary, assetManifestHash string) (*Revision, error) {
	revisionID, err := revisionGenerateUniqueID()
	if err != nil {
		return nil, fmt.Errorf("generate revision id: %w", err)
	}

	return &Revision{
		ID:                   RevisionIDFromString(revisionID),
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
		return ErrRevisionStateRequired
	}

	raw, err := s.pages.ReadPageRaw(pageID)
	if err != nil {
		return err
	}

	doc, _, err := revisionParsePageDocument(raw)
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
	raw, err := revisionJSONMarshal(meta)
	if err != nil {
		return "", fmt.Errorf("marshal page metadata: %w", err)
	}
	return sha256HexBytes(raw), nil
}

func hashExtraFrontmatter(extra map[string]interface{}) (string, error) {
	if len(extra) == 0 {
		return "", nil
	}

	raw, err := revisionJSONMarshal(extra)
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
		raw, err := revisionRenderPageDocument(markdown.PageDocument{
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

	raw, err := revisionBuildMarkdownWithMetadata(markdown.Frontmatter{
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

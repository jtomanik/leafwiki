package revision

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/perber/wiki/internal/core/tree"
)

func (s *Service) persistLiveAssets(pageID tree.PageID, refs []AssetRef) error {
	if len(refs) == 0 {
		return nil
	}

	for _, ref := range refs {
		srcPath := filepath.Join(s.liveAssetDir(pageID), ref.Name)
		hash, size, err := revisionStoreSaveAssetBlobFromPath(s.store, srcPath)
		if err != nil {
			return err
		}
		if hash != ref.SHA256 {
			return fmt.Errorf("%w: asset %s computed=%s saved=%s", ErrAssetBlobHashMismatch, ref.Name, ref.SHA256, hash)
		}
		if size != ref.SizeBytes {
			return fmt.Errorf("%w: asset %s computed=%d saved=%d", ErrAssetBlobSizeMismatch, ref.Name, ref.SizeBytes, size)
		}
	}
	return nil
}

func (s *Service) scanLiveAssets(pageID tree.PageID) ([]AssetRef, error) {
	dir := s.liveAssetDir(pageID)
	entries, err := revisionReadDir(dir)
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
	file, err := revisionOpen(absPath)
	if err != nil {
		return AssetRef{}, fmt.Errorf("open asset %s: %w", absPath, err)
	}
	defer func() { _ = revisionFileClose(file) }()

	hasher := sha256.New()
	size, err := revisionCopy(hasher, file)
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

	raw, err := revisionJSONMarshal(assetManifest{Items: canonical})
	if err != nil {
		return "", fmt.Errorf("marshal asset manifest for hash: %w", err)
	}

	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Service) restoreAssets(pageID tree.PageID, refs []AssetRef) error {
	dir := s.liveAssetDir(pageID)
	if err := revisionRemoveAll(dir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reset live asset dir: %w", err)
	}
	if len(refs) == 0 {
		return nil
	}
	if err := revisionMkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("ensure live asset dir: %w", err)
	}

	seen := make(map[string]struct{}, len(refs))
	for _, ref := range refs {
		name := strings.TrimSpace(ref.Name)
		if name == "" || filepath.Base(name) != name || strings.Contains(name, string(os.PathSeparator)) {
			return fmt.Errorf("%w: %s", ErrInvalidAssetName, ref.Name)
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("%w: %s", ErrDuplicateAssetName, name)
		}
		seen[name] = struct{}{}

		if err := revisionStoreCopyAssetBlobToPath(s.store, ref.SHA256, ref.SizeBytes, filepath.Join(dir, name)); err != nil {
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

	contentHash, err := revisionStoreSaveContentBlob(s.store, []byte(state.Content))
	if err != nil {
		return err
	}
	if contentHash != state.ContentHash {
		return fmt.Errorf("%w: computed=%s saved=%s", ErrContentHashMismatch, state.ContentHash, contentHash)
	}

	if err := s.persistLiveAssets(pageID, state.Assets); err != nil {
		return err
	}

	savedManifestHash, err := revisionStoreSaveAssetManifest(s.store, state.Assets)
	if err != nil {
		return err
	}
	if savedManifestHash != state.AssetManifestHash {
		return fmt.Errorf("%w: computed=%s saved=%s", ErrAssetManifestHashMismatch, state.AssetManifestHash, savedManifestHash)
	}

	rev, err := s.newRevision(RevisionTypeRestore, state, authorID, "page restored", savedManifestHash)
	if err != nil {
		return err
	}
	return revisionStoreSaveRevision(s.store, rev)
}

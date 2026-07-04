package revision

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/tree"
)

// CopyAssetBlobToPath streams the asset blob identified by hash to dstPath,
// verifying hash and size during the copy. The write is atomic (temp + rename).
func (s *FSStore) CopyAssetBlobToPath(hash string, expectedSize int64, dstPath string) error {
	hash = strings.ToLower(strings.TrimSpace(hash))
	src, err := s.OpenAssetBlob(hash)
	if err != nil {
		return err
	}
	defer func() { _ = revisionFileClose(src) }()

	tmpDir := filepath.Dir(dstPath)
	tmp, err := revisionCreateTemp(tmpDir, "asset-restore-*")
	if err != nil {
		return fmt.Errorf("create temp restore file: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = revisionFileClose(tmp); _ = revisionRemove(tmpName) }

	hasher := sha256.New()
	written, err := revisionCopy(io.MultiWriter(tmp, hasher), src)
	if err != nil {
		cleanup()
		return fmt.Errorf("stream asset blob to %s: %w", dstPath, err)
	}
	if err := revisionFileChmod(tmp, 0o644); err != nil {
		cleanup()
		return fmt.Errorf("chmod restored asset: %w", err)
	}
	if err := revisionFileClose(tmp); err != nil {
		_ = revisionRemove(tmpName)
		return fmt.Errorf("close temp restore file: %w", err)
	}
	if computedHash := hex.EncodeToString(hasher.Sum(nil)); computedHash != hash {
		_ = revisionRemove(tmpName)
		return fmt.Errorf("%w: computed %s, want %s", ErrAssetBlobHashMismatch, computedHash, hash)
	}
	if written != expectedSize {
		_ = revisionRemove(tmpName)
		return fmt.Errorf("%w: got %d, want %d", ErrAssetBlobSizeMismatch, written, expectedSize)
	}
	if err := revisionRename(tmpName, dstPath); err != nil {
		_ = revisionRemove(tmpName)
		return fmt.Errorf("move restored asset into place: %w", err)
	}
	return nil
}

func (s *FSStore) DeletePageRevisions(pageID tree.PageID) error {
	if err := validateStorageID(pageIDStorageKey(pageID)); err != nil {
		return fmt.Errorf("invalid page ID: %w", err)
	}

	if err := revisionRemoveAll(s.revisionsPageDir(pageID)); err != nil {
		return fmt.Errorf("delete page revisions: %w", err)
	}
	return nil
}

func (s *FSStore) baseDir() string {
	return filepath.Join(s.storageDir, ".leafwiki")
}

func (s *FSStore) revisionsDir() string {
	return filepath.Join(s.baseDir(), "revisions")
}

func (s *FSStore) revisionsPageDir(pageID tree.PageID) string {
	return filepath.Join(s.revisionsDir(), pageIDStorageKey(pageID))
}

func (s *FSStore) revisionFilePath(pageID tree.PageID, revisionID RevisionID, createdAt time.Time) string {
	filename := fmt.Sprintf("%s_%s.json", revisionFileTimestamp(createdAt), revisionIDStorageKey(revisionID))
	return filepath.Join(s.revisionsPageDir(pageID), filename)
}

func (s *FSStore) contentBlobPath(hash string) string {
	return filepath.Join(s.baseDir(), "blobs", "content", "sha256", shardHash(hash), hash)
}

func (s *FSStore) AssetBlobPath(hash string) string {
	return filepath.Join(s.baseDir(), "blobs", "assets", "sha256", shardHash(hash), hash)
}

func (s *FSStore) AssetManifestExists(hash string) bool {
	if hash == "" {
		return false
	}
	_, err := revisionStat(s.assetManifestPath(hash))
	return err == nil
}

func (s *FSStore) assetManifestPath(hash string) string {
	return filepath.Join(s.baseDir(), "manifests", "assets", "sha256", shardHash(hash), hash+".json")
}

// validateStorageID checks that an ID is safe to use as a single file path component.
// Rejects empty strings, path separators, and dot-only segments like "." or "..".
func validateStorageID(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("%w: id must not be empty", ErrRevisionValidation)
	}
	if strings.ContainsAny(id, "/\\") {
		return fmt.Errorf("%w: id must not contain path separators", ErrRevisionValidation)
	}
	if id == "." || id == ".." {
		return fmt.Errorf("%w: id must not be a dot component", ErrRevisionValidation)
	}
	return nil
}
func shardHash(hash string) string {
	if len(hash) < 2 {
		return "00"
	}
	return hash[:2]
}

func revisionFileTimestamp(ts time.Time) string {
	return ts.UTC().Format("20060102T150405.000000000Z0700")
}

func writeJSONAtomic(dst string, value any) error {
	raw, err := revisionJSONMarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, byte('\n'))
	return revisionWriteFileAtomic(dst, raw, 0o644)
}

func readJSON(path string, out any) error {
	raw, err := revisionReadFile(path)
	if err != nil {
		return err
	}
	if err := revisionJSONUnmarshal(raw, out); err != nil {
		return err
	}
	return nil
}

func sha256HexBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cloneAndSortAssetRefs(items []AssetRef) []AssetRef {
	cloned := make([]AssetRef, len(items))
	copy(cloned, items)

	sort.SliceStable(cloned, func(i, j int) bool {
		if cloned[i].Name == cloned[j].Name {
			return cloned[i].SHA256 < cloned[j].SHA256
		}
		return cloned[i].Name < cloned[j].Name
	})

	return cloned
}

func fileExists(path string) bool {
	_, err := revisionStat(path)
	return err == nil
}

func (s *FSStore) revisionIndexPath(pageID tree.PageID) string {
	return filepath.Join(s.revisionsPageDir(pageID), revisionIndexFileName)
}

func (s *FSStore) loadRevisionIndex(pageID tree.PageID) (revisionIndex, error) {
	path := s.revisionIndexPath(pageID)
	var index revisionIndex
	if err := readJSON(path, &index); err != nil {
		if os.IsNotExist(err) {
			return revisionIndex{}, nil
		}
		return nil, fmt.Errorf("read revision index: %w", err)
	}
	if index == nil {
		return revisionIndex{}, nil
	}
	return index, nil
}

func (s *FSStore) saveRevisionIndex(pageID tree.PageID, index revisionIndex) error {
	if index == nil {
		index = revisionIndex{}
	}
	path := s.revisionIndexPath(pageID)
	if err := revisionMkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("ensure revision dir: %w", err)
	}
	if err := writeJSONAtomic(path, index); err != nil {
		return fmt.Errorf("write revision index: %w", err)
	}
	return nil
}

func pageIDStorageKey(pageID tree.PageID) string {
	return pageID.MetadataValue()
}

func revisionIDStorageKey(revisionID RevisionID) string {
	return revisionID.CommitID()
}

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

type FSStore struct {
	storageDir string
}

type revisionIndex map[string]string

const revisionIndexFileName = "_index.json"

func NewFSStore(storageDir string) *FSStore {
	return &FSStore{storageDir: storageDir}
}

func (s *FSStore) SaveContentBlob(content []byte) (string, error) {
	hash := sha256HexBytes(content)
	dst := s.contentBlobPath(hash)

	if fileExists(dst) {
		return hash, nil
	}
	if err := revisionMkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", fmt.Errorf("ensure content blob dir: %w", err)
	}
	if err := revisionWriteFileAtomic(dst, content, 0o644); err != nil {
		if fileExists(dst) {
			return hash, nil
		}
		return "", fmt.Errorf("write content blob: %w", err)
	}

	return hash, nil
}

func (s *FSStore) SaveAssetBlobFromPath(srcPath string) (string, int64, error) {
	src, err := revisionOpen(srcPath)
	if err != nil {
		return "", 0, fmt.Errorf("open live asset %s: %w", srcPath, err)
	}
	defer func() { _ = revisionFileClose(src) }()

	tmpDir := filepath.Join(s.baseDir(), "tmp")
	if err := revisionMkdirAll(tmpDir, 0o755); err != nil {
		return "", 0, fmt.Errorf("ensure tmp dir: %w", err)
	}

	tmp, err := revisionCreateTemp(tmpDir, "asset-blob-*")
	if err != nil {
		return "", 0, fmt.Errorf("create temp asset blob: %w", err)
	}

	cleanupTmp := func() {
		_ = revisionFileClose(tmp)
		_ = revisionRemove(tmp.Name())
	}

	hasher := sha256.New()
	written, err := revisionCopy(io.MultiWriter(tmp, hasher), src)
	if err != nil {
		cleanupTmp()
		return "", 0, fmt.Errorf("copy asset to temp blob: %w", err)
	}

	hash := hex.EncodeToString(hasher.Sum(nil))
	dst := s.AssetBlobPath(hash)

	if err := revisionFileChmod(tmp, 0o644); err != nil {
		cleanupTmp()
		return "", 0, fmt.Errorf("chmod temp asset blob: %w", err)
	}
	if err := revisionFileClose(tmp); err != nil {
		_ = revisionRemove(tmp.Name())
		return "", 0, fmt.Errorf("close temp asset blob: %w", err)
	}

	if fileExists(dst) {
		_ = revisionRemove(tmp.Name())
		return hash, written, nil
	}

	if err := revisionMkdirAll(filepath.Dir(dst), 0o755); err != nil {
		_ = revisionRemove(tmp.Name())
		return "", 0, fmt.Errorf("ensure asset blob dir: %w", err)
	}

	if err := revisionRename(tmp.Name(), dst); err != nil {
		if fileExists(dst) {
			_ = revisionRemove(tmp.Name())
			return hash, written, nil
		}
		_ = revisionRemove(tmp.Name())
		return "", 0, fmt.Errorf("move asset blob into place: %w", err)
	}

	return hash, written, nil
}

func (s *FSStore) SaveAssetManifest(items []AssetRef) (string, error) {
	canonical := cloneAndSortAssetRefs(items)

	raw, err := revisionJSONMarshal(assetManifest{Items: canonical})
	if err != nil {
		return "", fmt.Errorf("marshal asset manifest: %w", err)
	}

	hash := sha256HexBytes(raw)
	dst := s.assetManifestPath(hash)

	if fileExists(dst) {
		return hash, nil
	}
	if err := revisionMkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", fmt.Errorf("ensure manifest dir: %w", err)
	}
	if err := revisionWriteFileAtomic(dst, raw, 0o644); err != nil {
		if fileExists(dst) {
			return hash, nil
		}
		return "", fmt.Errorf("write asset manifest: %w", err)
	}

	return hash, nil
}

func (s *FSStore) SaveRevision(rev *Revision) error {
	if rev == nil {
		return fmt.Errorf("%w: revision is required", ErrRevisionValidation)
	}
	if strings.TrimSpace(revisionIDStorageKey(rev.ID)) == "" {
		return fmt.Errorf("%w: revision id is required", ErrRevisionValidation)
	}
	if err := validateStorageID(pageIDStorageKey(rev.PageID)); err != nil {
		return fmt.Errorf("page id is required: %w", err)
	}
	if rev.CreatedAt.IsZero() {
		return ErrRevisionCreatedAtRequired
	}
	if err := validateStorageID(revisionIDStorageKey(rev.ID)); err != nil {
		return fmt.Errorf("invalid revision id %s: %w", rev.ID, err)
	}

	dst := s.revisionFilePath(rev.PageID, rev.ID, rev.CreatedAt)
	if err := revisionMkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("ensure revision dir: %w", err)
	}
	if err := writeJSONAtomic(dst, rev); err != nil {
		return fmt.Errorf("write revision: %w", err)
	}
	index, err := s.loadRevisionIndex(rev.PageID)
	if err != nil {
		return err
	}
	index[strings.TrimSpace(revisionIDStorageKey(rev.ID))] = filepath.Base(dst)
	if err := s.saveRevisionIndex(rev.PageID, index); err != nil {
		return err
	}
	return nil
}

func (s *FSStore) ListRevisions(pageID tree.PageID) ([]*Revision, error) {
	revisions, _, err := s.ListRevisionsPage(pageID, "", 0)
	if err != nil {
		return nil, err
	}
	return revisions, nil
}

func (s *FSStore) ListRevisionsPage(pageID tree.PageID, cursor string, pageSize RevisionListLimit) ([]*Revision, string, error) {
	if err := validateStorageID(pageIDStorageKey(pageID)); err != nil {
		return nil, "", fmt.Errorf("invalid page ID: %w", err)
	}
	names, err := s.revisionFileNames(pageID)
	if err != nil {
		return nil, "", err
	}
	if len(names) == 0 {
		return []*Revision{}, "", nil
	}

	start := 0
	cursor = strings.TrimSpace(cursor)
	if cursor != "" {
		start = len(names)
		for i, name := range names {
			if name == cursor {
				start = i + 1
				break
			}
		}
		if start >= len(names) {
			return []*Revision{}, "", nil
		}
	}

	end := len(names)
	if pageSize > 0 && start+int(pageSize) < end {
		end = start + int(pageSize)
	}

	dir := s.revisionsPageDir(pageID)
	revisions := make([]*Revision, 0, end-start)
	for _, name := range names[start:end] {
		var rev Revision
		if err := readJSON(filepath.Join(dir, name), &rev); err != nil {
			return nil, "", fmt.Errorf("read revision %s: %w", name, err)
		}
		revisions = append(revisions, &rev)
	}

	nextCursor := ""
	if end < len(names) {
		nextCursor = names[end-1]
	}
	return revisions, nextCursor, nil
}

func (s *FSStore) GetLatestRevision(pageID tree.PageID) (*Revision, error) {
	if err := validateStorageID(pageIDStorageKey(pageID)); err != nil {
		return nil, fmt.Errorf("invalid page ID: %w", err)
	}
	names, err := s.revisionFileNames(pageID)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		var latest *Revision
		return latest, nil
	}
	var rev Revision
	if err := readJSON(filepath.Join(s.revisionsPageDir(pageID), names[0]), &rev); err != nil {
		return nil, fmt.Errorf("read latest revision %s: %w", names[0], err)
	}
	return &rev, nil
}

func (s *FSStore) GetRevision(pageID tree.PageID, revisionID RevisionID) (*Revision, error) {
	if err := validateStorageID(pageIDStorageKey(pageID)); err != nil {
		return nil, fmt.Errorf("invalid page ID: %w", err)
	}
	revisionIDKey := strings.TrimSpace(revisionIDStorageKey(revisionID))
	if revisionIDKey == "" {
		return nil, os.ErrNotExist
	}

	index, err := s.loadRevisionIndex(pageID)
	if err != nil {
		return nil, err
	}
	if name := strings.TrimSpace(index[revisionIDKey]); name != "" {
		var rev Revision
		if err := readJSON(filepath.Join(s.revisionsPageDir(pageID), name), &rev); err != nil {
			return nil, fmt.Errorf("read revision %s: %w", name, err)
		}
		return &rev, nil
	}

	names, err := s.revisionFileNames(pageID)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if strings.HasSuffix(name, "_"+revisionIDKey+".json") {
			var rev Revision
			if err := readJSON(filepath.Join(s.revisionsPageDir(pageID), name), &rev); err != nil {
				return nil, fmt.Errorf("read revision %s: %w", name, err)
			}
			index[revisionIDKey] = name
			_ = s.saveRevisionIndex(pageID, index)
			return &rev, nil
		}
	}
	return nil, os.ErrNotExist
}

// PruneRevisions removes the oldest revision files beyond keepCount for the given page.
// Files are sorted newest-first, so names[keepCount:] are the oldest ones.
// Content blobs and asset manifests are NOT deleted — they are content-addressed and
// may be shared across multiple revisions.
func (s *FSStore) PruneRevisions(pageID tree.PageID, keepCount int) error {
	if keepCount <= 0 {
		return nil
	}
	if err := validateStorageID(pageIDStorageKey(pageID)); err != nil {
		return fmt.Errorf("invalid page ID: %w", err)
	}
	names, err := s.revisionFileNames(pageID)
	if err != nil || len(names) <= keepCount {
		return err
	}

	index, err := s.loadRevisionIndex(pageID)
	if err != nil {
		return err
	}

	// Build reverse map: filename → revisionID for index cleanup
	filenameToID := make(map[string]string, len(index))
	for id, filename := range index {
		filenameToID[filename] = id
	}

	toDelete := names[keepCount:]
	dir := s.revisionsPageDir(pageID)
	indexChanged := false

	for _, name := range toDelete {
		if err := revisionRemove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("delete revision file %s: %w", name, err)
		}
		if id, ok := filenameToID[name]; ok {
			delete(index, id)
			indexChanged = true
		}
	}

	if indexChanged {
		return s.saveRevisionIndex(pageID, index)
	}
	return nil
}

func (s *FSStore) revisionFileNames(pageID tree.PageID) ([]string, error) {
	dir := s.revisionsPageDir(pageID)
	entries, err := revisionReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("read revisions dir: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") || entry.Name() == revisionIndexFileName {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names, nil
}

func (s *FSStore) ReadContentBlob(hash string) ([]byte, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return []byte{}, nil
	}

	raw, err := revisionReadFile(s.contentBlobPath(hash))
	if err != nil {
		return nil, fmt.Errorf("read content blob: %w", err)
	}
	return raw, nil
}

// OpenContentBlob returns a streaming reader for the content blob.
// The caller is responsible for closing the returned ReadCloser.
// Use this instead of ReadContentBlob when you don't need the full content in memory.
func (s *FSStore) OpenContentBlob(hash string) (io.ReadCloser, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return io.NopCloser(strings.NewReader("")), nil
	}
	f, err := revisionOpen(s.contentBlobPath(hash))
	if err != nil {
		return nil, fmt.Errorf("open content blob: %w", err)
	}
	return f, nil
}

func (s *FSStore) LoadAssetManifest(hash string) ([]AssetRef, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return []AssetRef{}, nil
	}

	var manifest assetManifest
	if err := readJSON(s.assetManifestPath(hash), &manifest); err != nil {
		return nil, fmt.Errorf("read asset manifest: %w", err)
	}
	return cloneAndSortAssetRefs(manifest.Items), nil
}

func (s *FSStore) ReadAssetBlob(hash string) ([]byte, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, ErrAssetHashRequired
	}

	raw, err := revisionReadFile(s.AssetBlobPath(hash))
	if err != nil {
		return nil, fmt.Errorf("read asset blob: %w", err)
	}
	return raw, nil
}

// OpenAssetBlob returns an open file handle for the given asset blob.
// The caller is responsible for closing the returned file.
func (s *FSStore) OpenAssetBlob(hash string) (*os.File, error) {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return nil, ErrAssetHashRequired
	}
	f, err := revisionOpen(s.AssetBlobPath(hash))
	if err != nil {
		return nil, fmt.Errorf("open asset blob: %w", err)
	}
	return f, nil
}

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

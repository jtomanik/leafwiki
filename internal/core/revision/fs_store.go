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

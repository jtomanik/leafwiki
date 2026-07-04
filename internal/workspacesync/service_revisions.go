package workspacesync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/revision"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/workspacesync/gitrevisions"
)

func (s *Service) ListPageRevisions(ctx context.Context, page *tree.Page, cursor string, pageSize PageRevisionLimit) (PageRevisionList, error) {
	s.mu.Lock()
	if !s.enabled || page == nil || page.PageNode == nil {
		s.mu.Unlock()
		return PageRevisionList{}, nil
	}
	store := s.store
	rootDir := s.rootDir
	s.mu.Unlock()

	requestedLimit := int(pageSize)
	if requestedLimit <= 0 {
		requestedLimit = 50
	}
	relPath := pageMarkdownPath(page)
	scanLimit := requestedLimit + 1
	revisions := make([]*revision.Revision, 0, scanLimit)
	cursorHash := CommitHashFromString(strings.TrimSpace(cursor))
	foundCursor := cursorHash == ""
	s.storeMu.Lock()
	err := store.ForEachCommit(ctx, func(commit gitrevisions.Commit) (bool, error) {
		if !foundCursor {
			if commit.Hash == cursorHash {
				foundCursor = true
			}
			return true, nil
		}
		changedFiles, err := store.ChangedMarkdownContents(ctx, commit.Hash)
		if err != nil {
			return false, err
		}
		content, revisionPath, ok := changedContentForPageAtCommit(rootDir, page, relPath, changedFiles)
		if !ok {
			return true, nil
		}
		revisions = append(revisions, revisionForPageContent(rootDir, page, commit, revisionPath, content))
		if len(revisions) >= scanLimit {
			return false, nil
		}
		return true, nil
	})
	s.storeMu.Unlock()
	if err != nil {
		return PageRevisionList{}, err
	}
	nextCursor := ""
	if len(revisions) > requestedLimit {
		nextCursor = revisions[requestedLimit-1].ID.CommitID()
		revisions = revisions[:requestedLimit]
	}
	return PageRevisionList{Revisions: revisions, NextCursor: nextCursor}, nil
}

func pageMarkdownPath(page *tree.Page) string {
	if sourcePath := pageWorkspaceSourcePath(page); sourcePath != "" {
		sourcePathFS := sourcePath.FilesystemPath()
		if page.Kind == tree.NodeKindSection {
			return joinWorkspaceMarkdownPath(sourcePathFS, "index.md")
		}
		return sourcePathFS
	}
	path := strings.TrimPrefix(page.CalculatePath(), "/")
	if page != nil && page.Kind == tree.NodeKindSection {
		if path == "" {
			return "index.md"
		}
		return path + "/index.md"
	}
	return path + ".md"
}

func (s *Service) currentPageMarkdownPath(page *tree.Page) string {
	preferred := pageMarkdownPath(page)
	rootDir := strings.TrimSpace(s.rootDir)
	if rootDir == "" {
		return preferred
	}
	if sourcePath := pageWorkspaceSourcePath(page); sourcePath != "" {
		sourcePathFS := sourcePath.FilesystemPath()
		if page.Kind == tree.NodeKindSection {
			return s.currentSectionContentPath(sourcePathFS, preferred)
		}
		return sourcePathFS
	}
	if mappedPath, ok := s.currentWorkspaceMarkdownPathByRoute(page); ok {
		return mappedPath
	}
	dir, base := filepath.Split(filepath.FromSlash(preferred))
	entries, err := os.ReadDir(filepath.Join(rootDir, dir))
	if err != nil {
		return preferred
	}
	readmeFallback := ""
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.EqualFold(entry.Name(), base) {
			if page.Kind == tree.NodeKindSection && entry.Name() == "README.md" {
				readmeFallback = filepath.ToSlash(filepath.Join(filepath.ToSlash(dir), entry.Name()))
			}
			continue
		}
		candidate := filepath.ToSlash(filepath.Join(filepath.ToSlash(dir), entry.Name()))
		if gitrevisions.IsManagedMarkdownRelPath(candidate) {
			return candidate
		}
	}
	if readmeFallback != "" && gitrevisions.IsManagedMarkdownRelPath(readmeFallback) {
		return readmeFallback
	}
	return preferred
}

func pageWorkspaceSourcePath(page *tree.Page) tree.WorkspaceSourcePath {
	if page == nil || page.PageNode == nil {
		return ""
	}
	return page.PageNode.WorkspaceSourcePath.Clean()
}

func cleanWorkspaceMarkdownPath(value string) string {
	return strings.Trim(strings.TrimSpace(filepath.ToSlash(value)), "/")
}

func joinWorkspaceMarkdownPath(parts ...string) string {
	nonEmpty := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(cleanWorkspaceMarkdownPath(part), "/")
		if part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	return strings.Join(nonEmpty, "/")
}

func (s *Service) currentSectionContentPath(sourcePath string, fallback string) string {
	dir := filepath.Join(s.rootDir, filepath.FromSlash(sourcePath))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fallback
	}
	readmeFallback := ""
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := path.Ext(name)
		base := strings.TrimSuffix(name, ext)
		if strings.EqualFold(base, "index") && strings.EqualFold(ext, ".md") {
			return joinWorkspaceMarkdownPath(sourcePath, name)
		}
		if name == "README.md" {
			readmeFallback = joinWorkspaceMarkdownPath(sourcePath, name)
		}
	}
	if readmeFallback != "" {
		return readmeFallback
	}
	return fallback
}

func (s *Service) currentWorkspaceMarkdownPathByRoute(page *tree.Page) (string, bool) {
	if page == nil || page.PageNode == nil {
		return "", false
	}
	targetRoutePath := tree.RoutePathFromString(strings.Trim(page.CalculatePath(), "/")).Clean()
	var found string
	err := workspacesyncWalkDir(s.rootDir, func(filePath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == s.rootDir {
			return nil
		}
		if entry.IsDir() {
			if strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		relPath, err := workspacesyncRel(s.rootDir, filePath)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		routePath, kind := revisionRoutePathAndKind(s.rootDir, relPath, page)
		if kind != page.Kind || routePath.Clean() != targetRoutePath {
			return nil
		}
		found = relPath
		return filepath.SkipAll
	})
	if err != nil || found == "" {
		return "", false
	}
	return found, true
}

func contentForPageAtCommit(rootDir string, page *tree.Page, files map[string]string) (string, string, bool) {
	return contentForPageAtCommitPath(rootDir, page, pageMarkdownPath(page), files)
}

func contentForPageAtCommitPath(rootDir string, page *tree.Page, preferredPath string, files map[string]string) (string, string, bool) {
	if content, ok := files[preferredPath]; ok {
		if contentMatchesLeafWikiID(page, content) {
			return content, preferredPath, true
		}
	}
	paths := sortedMarkdownPaths(files)
	for _, markdownPath := range paths {
		content := files[markdownPath]
		if markdownPathMatchesPageRoute(rootDir, page, markdownPath) && contentMatchesLeafWikiID(page, content) {
			return content, markdownPath, true
		}
	}
	for _, markdownPath := range paths {
		content := files[markdownPath]
		leafWikiID, ok := leafWikiIDFromContent(content)
		if !ok {
			continue
		}
		if leafWikiID == page.ID {
			return content, markdownPath, true
		}
	}
	return "", "", false
}

func changedContentForPageAtCommit(rootDir string, page *tree.Page, preferredPath string, changedFiles map[string]string) (string, string, bool) {
	if len(changedFiles) == 0 {
		return "", "", false
	}
	if content, ok := changedFiles[preferredPath]; ok {
		if contentMatchesLeafWikiID(page, content) {
			return content, preferredPath, true
		}
	}
	paths := sortedMarkdownPaths(changedFiles)
	for _, markdownPath := range paths {
		content := changedFiles[markdownPath]
		if markdownPathMatchesPageRoute(rootDir, page, markdownPath) && contentMatchesLeafWikiID(page, content) {
			return content, markdownPath, true
		}
	}
	for _, markdownPath := range paths {
		content := changedFiles[markdownPath]
		leafWikiID, ok := leafWikiIDFromContent(content)
		if !ok {
			continue
		}
		if leafWikiID == page.ID {
			return content, markdownPath, true
		}
	}
	return "", "", false
}

func sortedMarkdownPaths(files map[string]string) []string {
	paths := make([]string, 0, len(files))
	for markdownPath := range files {
		if !gitrevisions.IsManagedMarkdownRelPath(markdownPath) {
			continue
		}
		paths = append(paths, markdownPath)
	}
	sort.Strings(paths)
	return paths
}

func markdownPathMatchesPageRoute(rootDir string, page *tree.Page, relPath string) bool {
	if page == nil || page.PageNode == nil {
		return false
	}
	routePath, kind := revisionRoutePathAndKind(rootDir, relPath, page)
	return kind == page.Kind && routePath.Clean() == tree.RoutePathFromString(strings.Trim(page.CalculatePath(), "/")).Clean()
}

func contentMatchesLeafWikiID(page *tree.Page, content string) bool {
	leafWikiID, ok := leafWikiIDFromContent(content)
	if !ok {
		return false
	}
	return leafWikiID == "" || leafWikiID == page.ID
}

func leafWikiIDFromContent(content string) (tree.PageID, bool) {
	doc, _, err := markdown.ParsePageDocument(content)
	if err != nil {
		return "", false
	}
	return tree.PageIDFromString(strings.TrimSpace(doc.Metadata.Page.ID)), true
}

func revisionForPageContent(rootDir string, page *tree.Page, commit gitrevisions.Commit, relPath string, content string) *revision.Revision {
	sum := sha256.Sum256([]byte(content))
	authorID := tree.UserIDFromString(commit.AuthorID.ActorID())
	if authorID == "" {
		authorID = tree.UserIDFromString(PublicEditorActor().ID)
	}
	summary := strings.TrimSpace(commit.Message)
	if summary == "" {
		summary = "workspace sync"
	}
	title := page.Title
	if mdFile, err := markdown.NewMarkdownFileFromRaw(relPath, content); err == nil {
		if historicalTitle, err := mdFile.GetTitle(); err == nil && strings.TrimSpace(historicalTitle) != "" {
			title = strings.TrimSpace(historicalTitle)
		}
	}
	routePath, slug, kind := revisionRoutePathSlugAndKind(rootDir, relPath, page)
	return &revision.Revision{
		ID:            tree.RevisionIDFromString(commit.Hash),
		PageID:        page.ID,
		Type:          revision.RevisionTypeContentUpdate,
		AuthorID:      authorID.MetadataValue(),
		CreatedAt:     commit.CreatedAt,
		Title:         title,
		Slug:          slug,
		Kind:          kind,
		Path:          routePath.FilesystemPath(),
		ContentHash:   hex.EncodeToString(sum[:]),
		PageCreatedAt: page.Metadata.CreatedAt,
		PageUpdatedAt: page.Metadata.UpdatedAt,
		CreatorID:     firstNonEmptyUserID(page.Metadata.CreatorID, authorID),
		LastAuthorID:  firstNonEmptyUserID(page.Metadata.LastAuthorID, authorID),
		Summary:       summary,
	}
}

func firstNonEmptyUserID(primary tree.UserID, fallback tree.UserID) string {
	if primary != "" {
		return primary.MetadataValue()
	}
	return fallback.MetadataValue()
}

func revisionRoutePathSlugAndKind(rootDir string, relPath string, page *tree.Page) (tree.RoutePath, tree.Slug, tree.NodeKind) {
	relPath = filepath.ToSlash(relPath)
	routePath, kind := revisionRoutePathAndKind(rootDir, relPath, page)
	slug := routePath.LeafSlug()
	if slug == "" && page != nil {
		slug = page.Slug
	}
	return routePath, slug, kind
}

func revisionRoutePathAndKind(rootDir string, relPath string, page *tree.Page) (tree.RoutePath, tree.NodeKind) {
	relPath = filepath.ToSlash(relPath)
	base := path.Base(relPath)
	dir := path.Dir(relPath)
	if dir == "." {
		dir = ""
	}
	dir = strings.Trim(dir, "/")
	route, err := tree.MapWorkspaceMarkdownRoute(rootDir, relPath, false)
	if err == nil && !route.Skip {
		return route.RoutePath, route.Kind
	}
	if isRevisionReadmeFallbackSection(relPath, page, dir) {
		return tree.RoutePathFromString(dir), tree.NodeKindSection
	}
	if strings.EqualFold(base, "index.md") {
		return tree.RoutePathFromString(dir), tree.NodeKindSection
	}
	return tree.CleanMarkdownPath(relPath).RoutePath(), tree.NodeKindPage
}

func isRevisionReadmeFallbackSection(relPath string, page *tree.Page, dir string) bool {
	if path.Base(filepath.ToSlash(relPath)) != "README.md" {
		return false
	}
	if page == nil || page.PageNode == nil || page.Kind != tree.NodeKindSection {
		return false
	}
	return tree.RoutePathFromString(strings.Trim(page.CalculatePath(), "/")).Clean() == tree.RoutePathFromString(dir).Clean()
}

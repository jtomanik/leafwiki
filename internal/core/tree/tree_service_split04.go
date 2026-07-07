package tree

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// BulkUpdateContent updates content for multiple pages under a single write lock,
// running disk writes in parallel. Returns per-item errors; nil means success.
// Only content and metadata timestamps are updated; slug and title are unchanged.
func (t *TreeService) BulkUpdateContent(userID UserID, updates []BulkContentUpdate) []error {
	errs := make([]error, len(updates))
	if len(updates) == 0 {
		return errs
	}

	type task struct {
		index       int
		node        *PageNode
		content     string
		oldMetadata PageMetadata
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.tree == nil {
		for i := range errs {
			errs[i] = ErrTreeNotLoaded
		}
		return errs
	}

	now := time.Now().UTC()
	tasks := make([]task, 0, len(updates))
	for i, u := range updates {
		node := t.getNodeByIDLocked(u.ID)
		if node == nil {
			errs[i] = ErrPageNotFound
			continue
		}
		oldMetadata := node.Metadata
		// Update in-memory metadata before disk write so UpsertContent writes the correct timestamps.
		node.Metadata.UpdatedAt = now
		node.Metadata.LastAuthorID = userID
		tasks = append(tasks, task{index: i, node: node, content: u.Content, oldMetadata: oldMetadata})
	}

	if len(tasks) == 0 {
		return errs
	}

	// Each page lives in its own file — writes are independent and safe to parallelise.
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for _, tk := range tasks {
		go func(tk task) {
			defer wg.Done()
			if err := treeStoreUpsertContent(t.store, tk.node, tk.content); err != nil {
				mu.Lock()
				errs[tk.index] = err
				mu.Unlock()
			}
		}(tk)
	}
	wg.Wait()

	for _, tk := range tasks {
		if errs[tk.index] != nil {
			tk.node.Metadata = tk.oldMetadata
		}
	}

	return errs
}

// GetPages returns pages for the given IDs under a single read lock,
// reading files in parallel. Each entry is nil when the corresponding error is non-nil.
func (t *TreeService) GetPages(ids []PageID) ([]*Page, []error) {
	pages := make([]*Page, len(ids))
	errs := make([]error, len(ids))
	if len(ids) == 0 {
		return pages, errs
	}

	type task struct {
		index int
		node  *PageNode
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.tree == nil {
		for i := range errs {
			errs[i] = ErrTreeNotLoaded
		}
		return pages, errs
	}

	tasks := make([]task, 0, len(ids))
	for i, id := range ids {
		node := t.getNodeByIDLocked(id)
		if node == nil {
			errs[i] = ErrPageNotFound
			continue
		}
		tasks = append(tasks, task{index: i, node: node})
	}

	if len(tasks) == 0 {
		return pages, errs
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(len(tasks))
	for _, tk := range tasks {
		go func(tk task) {
			defer wg.Done()
			content, raw, err := treeStoreReadPageAndRaw(t.store, tk.node)
			mu.Lock()
			if err != nil {
				errs[tk.index] = fmt.Errorf("%w: %w", ErrGetPageContent, err)
			} else {
				pages[tk.index] = &Page{PageNode: tk.node, Content: content, RawContent: raw}
			}
			mu.Unlock()
		}(tk)
	}
	wg.Wait()

	return pages, errs
}

// GetPage returns a page by its ID
func (t *TreeService) GetPage(id PageID) (*Page, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.tree == nil {
		return nil, ErrTreeNotLoaded
	}

	// Find the page
	page := t.getNodeByIDLocked(id)
	if page == nil {
		return nil, ErrPageNotFound
	}

	content, raw, err := treeStoreReadPageAndRaw(t.store, page)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGetPageContent, err)
	}

	return &Page{
		PageNode:   page,
		Content:    content,
		RawContent: raw,
	}, nil
}

// ReadPageRaw returns the raw markdown of a page, including metadata.
func (t *TreeService) ReadPageRaw(id PageID) (string, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.tree == nil {
		return "", ErrTreeNotLoaded
	}

	page := t.getNodeByIDLocked(id)
	if page == nil {
		return "", ErrPageNotFound
	}

	raw, err := treeStoreReadPageRaw(t.store, page)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrGetPageRawContent, err)
	}

	return raw, nil
}

// ResolvePermalinkTarget resolves a stable page ID to the current route path.
func (t *TreeService) ResolvePermalinkTarget(id PageID) (*PermalinkTarget, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.tree == nil {
		return nil, ErrTreeNotLoaded
	}

	node := t.getNodeByIDLocked(id)
	if node == nil {
		return nil, ErrPageNotFound
	}

	return &PermalinkTarget{
		ID:   node.ID,
		Slug: node.Slug,
		Path: strings.TrimPrefix(node.CalculatePath(), "/"),
		Kind: node.Kind,
	}, nil
}

// FindPageByRoutePath finds a page in the tree by its path.
func (t *TreeService) FindPageByRoutePath(routePath RoutePath) (*Page, error) {
	return t.findPageByRoutePath(routePath, "")
}

// FindPageByRoutePathAndKind finds a page in the tree by path and final node kind.
func (t *TreeService) FindPageByRoutePathAndKind(routePath RoutePath, kind NodeKind) (*Page, error) {
	return t.findPageByRoutePath(routePath, kind)
}

func (t *TreeService) findPageByRoutePath(routePath RoutePath, finalKind NodeKind) (*Page, error) {
	parsedRoutePath, err := routePath.Validate()
	if err != nil {
		return nil, err
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.tree == nil {
		return nil, ErrTreeNotLoaded
	}

	// Split the routePath into parts
	routePart := parsedRoutePath.Segments()

	parent := t.tree
	var node *PageNode
	for index, part := range routePart {
		if index == len(routePart)-1 && finalKind != "" {
			node = t.findChildBySlugAndKindExactInParentLocked(parent, part, finalKind)
		} else if index < len(routePart)-1 {
			node = t.findChildBySlugAndKindExactInParentLocked(parent, part, NodeKindSection)
			if node == nil {
				node = t.findChildBySlugExactInParentLocked(parent, part)
			}
		} else {
			node = t.findRouteSegmentExactInParentLocked(parent, part)
		}
		if node == nil {
			return nil, ErrPageNotFound
		}

		parent = node
	}

	content, err := treeStoreReadPageContent(t.store, node)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrGetPageContent, err)
	}

	return &Page{
		PageNode: node,
		Content:  content,
	}, nil
}

// LookupPagePath looks up a path in the tree and returns a PathLookup struct
// that contains information about the path and its segments and whether they exist.
func (t *TreeService) LookupPagePath(p RoutePath) (*PathLookup, error) {
	routePath, err := p.Validate()
	if err != nil {
		return nil, err
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	var finalKind NodeKind
	return t.lookupPagePathLocked(routePath, finalKind)
}

// LookupPagePathForKind looks up a path while requiring the final segment to
// match the requested kind when it exists.
func (t *TreeService) LookupPagePathForKind(p RoutePath, finalKind NodeKind) (*PathLookup, error) {
	routePath, err := p.Validate()
	if err != nil {
		return nil, err
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.lookupPagePathLocked(routePath, finalKind)
}

// lookupPagePathLocked looks up a path in the tree and returns a PathLookup struct
// that contains information about the path and its segments and whether they exist.
// Lock must be held by the caller.
func (t *TreeService) lookupPagePathLocked(p RoutePath, finalKind NodeKind) (*PathLookup, error) {
	if t.tree == nil {
		return nil, ErrTreeNotLoaded
	}

	slugService := NewSlugService()
	path := p.Clean()
	if path == "" {
		return &PathLookup{
			Path:      path,
			Segments:  []PathSegment{},
			Exists:    false,
			CanCreate: false,
		}, nil
	}

	routePath, err := path.Validate()
	if err != nil {
		return nil, err
	}
	// Split the path into parts
	pathParts := routePath.Segments()

	lookup := &PathLookup{
		Path:      routePath,
		Segments:  make([]PathSegment, len(pathParts)),
		Exists:    true,
		CanCreate: true,
	}

	parent := t.tree

	// Check each segment in the path
	for i, part := range pathParts {
		// Find the segment in the tree
		segment := PathSegment{
			Slug:   part,
			Exists: false,
		}

		// push the segment to the lookup
		lookup.Segments[i] = segment

		// Check if the segment exists under the current parent.
		var e *PageNode
		if i == len(pathParts)-1 && finalKind != "" {
			e = t.findChildBySlugAndKindInParentLocked(parent, part, finalKind)
		} else {
			e = t.findRouteSegmentInParentLocked(parent, part)
		}
		if e != nil {
			// Segment exists
			lookup.Segments[i].Exists = true
			lookup.Segments[i].ID = &e.ID
			lookup.Segments[i].Kind = &e.Kind
			lookup.Segments[i].Title = &e.Title

			// Move to the next parent
			parent = e
		}

		// If the segment does not exist, set the pathExists flag to false
		if !lookup.Segments[i].Exists {
			if lookup.CanCreate && slugService.IsValidSlug(part.FilesystemPath()) != nil {
				lookup.CanCreate = false
			}

			// No need to check further segments
			// Set all remaining segments to non-existing
			for j := i + 1; j < len(pathParts); j++ {
				if lookup.CanCreate && slugService.IsValidSlug(pathParts[j].FilesystemPath()) != nil {
					lookup.CanCreate = false
				}
				lookup.Segments[j] = PathSegment{
					Slug:   pathParts[j],
					Exists: false,
				}
			}

			lookup.Exists = false

			parent = nil
		}
	}

	return lookup, nil
}

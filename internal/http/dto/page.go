// Package dto contains the HTTP response types and mapping functions shared
// across all domain route registrars. It must NOT import internal/wiki to
// avoid circular dependencies.
package dto

import (
	pathpkg "path"
	"time"

	"github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/tree"
)

// ContentPathResolver returns the Markdown path backing a node, relative to the
// configured wiki root.
type ContentPathResolver func(*tree.PageNode) (string, error)

// NodeMetadata contains authorship and timestamp information for a page node.
type NodeMetadata struct {
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
	CreatorID    string `json:"creatorId"`
	LastAuthorID string `json:"lastAuthorId"`

	Creator    *auth.UserLabel `json:"creator,omitempty"`
	LastAuthor *auth.UserLabel `json:"lastAuthor,omitempty"`
}

// Node is the HTTP representation of a page tree node.
type Node struct {
	ID       string        `json:"id"`
	Title    string        `json:"title"`
	Slug     string        `json:"slug"`
	Path     string        `json:"path"`
	Version  string        `json:"version"`
	Position int           `json:"position"`
	Kind     tree.NodeKind `json:"kind"`
	// ContentPath is the Markdown file backing this node when the mapper has
	// enough filesystem context to distinguish index.md from README.md fallback.
	ContentPath    string       `json:"contentPath,omitempty"`
	ReadmeFallback bool         `json:"readmeFallback,omitempty"`
	Children       []*Node      `json:"children"`
	Metadata       NodeMetadata `json:"metadata"`
}

// Page is the HTTP representation of a full page (node + content).
type Page struct {
	*Node
	Content    string            `json:"content"`
	Path       string            `json:"path"`
	Tags       []string          `json:"tags"`
	Properties map[string]string `json:"properties"`
}

// ToAPIPage converts a tree.Page to its HTTP representation.
func ToAPIPage(p *tree.Page, userResolver *auth.UserResolver) *Page {
	return &Page{
		Node:       ToAPINode(p.PageNode, "", userResolver),
		Content:    p.Content,
		Path:       BuildPathFromNode(p.PageNode),
		Tags:       []string{},
		Properties: map[string]string{},
	}
}

// ToAPIPageWithDepth converts a tree.Page with a depth-limited node tree.
func ToAPIPageWithDepth(p *tree.Page, userResolver *auth.UserResolver, depth int) *Page {
	return &Page{
		Node:       ToAPINodeWithDepth(p.PageNode, "", userResolver, depth),
		Content:    p.Content,
		Path:       BuildPathFromNode(p.PageNode),
		Tags:       []string{},
		Properties: map[string]string{},
	}
}

// BuildPathFromNode builds the slash-separated path string from a node.
func BuildPathFromNode(node *tree.PageNode) string {
	return node.CalculateRoutePath().FilesystemPath()
}

// ToAPINode recursively converts a tree.PageNode to its HTTP representation.
func ToAPINode(node *tree.PageNode, parentPath string, userResolver *auth.UserResolver) *Node {
	return toAPINode(node, parentPath, userResolver, nil)
}

// ToAPINodeWithContentPaths recursively converts a tree.PageNode and includes
// each node's backing Markdown file path.
func ToAPINodeWithContentPaths(node *tree.PageNode, parentPath string, userResolver *auth.UserResolver, contentPathResolver ContentPathResolver) *Node {
	return toAPINode(node, parentPath, userResolver, contentPathResolver)
}

func toAPINode(node *tree.PageNode, parentPath string, userResolver *auth.UserResolver, contentPathResolver ContentPathResolver) *Node {
	path := node.CalculateRoutePath().FilesystemPath()

	var creator, lastAuthor *auth.UserLabel
	if userResolver != nil {
		creator, _ = userResolver.ResolveUserLabel(auth.NewUserIDUnchecked(node.Metadata.CreatorID.MetadataValue()))
		lastAuthor, _ = userResolver.ResolveUserLabel(auth.NewUserIDUnchecked(node.Metadata.LastAuthorID.MetadataValue()))
	}

	contentPath := contentPathForNode(node, contentPathResolver)
	apiNode := &Node{
		ID:             node.ID.String(),
		Title:          node.Title,
		Slug:           node.Slug.String(),
		Path:           path,
		Version:        node.Version().String(),
		Position:       node.Position,
		Kind:           node.Kind,
		ContentPath:    contentPath,
		ReadmeFallback: node.Kind == tree.NodeKindSection && pathpkg.Base(contentPath) == "README.md",
		Metadata: NodeMetadata{
			CreatedAt:    node.Metadata.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    node.Metadata.UpdatedAt.Format(time.RFC3339),
			CreatorID:    node.Metadata.CreatorID.String(),
			LastAuthorID: node.Metadata.LastAuthorID.String(),
			Creator:      creator,
			LastAuthor:   lastAuthor,
		},
	}

	for _, child := range node.Children {
		apiNode.Children = append(apiNode.Children, toAPINode(child, path, userResolver, contentPathResolver))
	}

	return apiNode
}

// pruneNodeDepth limits the node tree to the given depth.
// depth == 0 → drop all children; depth < 0 → unlimited.
func pruneNodeDepth(n *Node, depth int) {
	if n == nil {
		return
	}
	if depth == 0 {
		n.Children = nil
		return
	}
	if depth < 0 {
		return
	}
	for _, child := range n.Children {
		pruneNodeDepth(child, depth-1)
	}
}

// ToAPINodeWithDepth converts a node with depth limiting.
func ToAPINodeWithDepth(node *tree.PageNode, parentPath string, userResolver *auth.UserResolver, depth int) *Node {
	apiNode := ToAPINode(node, parentPath, userResolver)
	if depth < 0 {
		return apiNode
	}
	pruneNodeDepth(apiNode, depth)
	return apiNode
}

// ToAPINodeWithContentPathsAndDepth converts a node with depth limiting and
// backing Markdown file paths.
func ToAPINodeWithContentPathsAndDepth(node *tree.PageNode, parentPath string, userResolver *auth.UserResolver, contentPathResolver ContentPathResolver, depth int) *Node {
	apiNode := ToAPINodeWithContentPaths(node, parentPath, userResolver, contentPathResolver)
	if depth < 0 {
		return apiNode
	}
	pruneNodeDepth(apiNode, depth)
	return apiNode
}

func contentPathForNode(node *tree.PageNode, contentPathResolver ContentPathResolver) string {
	if contentPathResolver == nil {
		return ""
	}
	contentPath, err := contentPathResolver(node)
	if err != nil {
		return ""
	}
	return contentPath
}

// FormatAPITime formats a time.Time to RFC3339 or empty string for zero time.
func FormatAPITime(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.Format(time.RFC3339)
}

package mcp

import (
	"context"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/http/dto"
	wikilinks "github.com/perber/wiki/internal/wiki/links"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
)

func (r *Routes) registerNavigationTools(server *sdkmcp.Server) {
	addTypedTool[getSubtreeInput, subtreeOutput](server, toolGetSubtree, func(ctx context.Context, in getSubtreeInput) (subtreeOutput, error) {
		return r.getSubtree(ctx, in)
	})
}

func (r *Routes) getSubtree(ctx context.Context, in getSubtreeInput) (subtreeOutput, error) {
	pageID := strings.TrimSpace(in.PageID)
	routePath := normalizeToolRoutePath(in.Path)
	if pageID != "" && routePath != "" {
		return subtreeOutput{}, fmt.Errorf("pageId and path cannot both be supplied")
	}
	depth, err := boundedSubtreeDepth(in.Depth)
	if err != nil {
		return subtreeOutput{}, err
	}
	opts := subtreeOptions{
		IncludeMetadata:       true,
		IncludeLinkCounts:     in.IncludeLinkCounts,
		IncludeContentPreview: in.IncludeContentPreview,
	}
	if in.IncludeMetadata != nil {
		opts.IncludeMetadata = *in.IncludeMetadata
	}

	var node *tree.PageNode
	switch {
	case pageID != "":
		page, err := r.treeService.GetPage(pageID)
		if err != nil {
			return subtreeOutput{}, err
		}
		node = page.PageNode
	case routePath != "":
		validPath, err := wikipages.ValidatePageRoutePath(routePath)
		if err != nil {
			return subtreeOutput{}, err
		}
		page, err := r.findByPath.Execute(ctx, wikipages.FindByPathInput{RoutePath: validPath})
		if err != nil {
			return subtreeOutput{}, err
		}
		node = page.Page.PageNode
	default:
		node = r.treeService.GetTree()
	}
	if node == nil {
		return subtreeOutput{}, fmt.Errorf("page not found")
	}

	root := r.subtreeNode(ctx, node, parentPathForNode(node), depth, opts)
	ensureSubtreeNodeChildrenArray(root)
	breadcrumbs := r.breadcrumbNodes(ctx, node, opts)
	return subtreeOutput{
		Root:        root,
		Breadcrumbs: breadcrumbs,
		Depth:       depth,
		Truncated:   subtreeTruncated(node, depth),
	}, nil
}

type subtreeOptions struct {
	IncludeMetadata       bool
	IncludeLinkCounts     bool
	IncludeContentPreview bool
}

func (r *Routes) subtreeNode(ctx context.Context, node *tree.PageNode, parentPath string, depth int, opts subtreeOptions) *subtreeNode {
	if node == nil {
		return nil
	}
	apiNode := dto.ToAPINodeWithDepth(node, parentPath, r.userResolver, 0)
	out := &subtreeNode{
		ID:       apiNode.ID,
		Title:    apiNode.Title,
		Slug:     apiNode.Slug,
		Path:     apiNode.Path,
		Version:  apiNode.Version,
		Position: apiNode.Position,
		Kind:     apiNode.Kind,
		Children: []*subtreeNode{},
	}
	if opts.IncludeMetadata {
		metadata := apiNode.Metadata
		out.Metadata = &metadata
	}
	if opts.IncludeLinkCounts {
		out.LinkCounts = r.subtreeLinkCounts(ctx, node)
	}
	if opts.IncludeContentPreview && node.Kind == tree.NodeKindPage {
		out.ContentPreview = r.subtreeContentPreview(node.ID)
	}
	if depth == 0 {
		return out
	}
	childDepth := depth - 1
	if depth < 0 {
		childDepth = -1
	}
	for _, child := range node.Children {
		out.Children = append(out.Children, r.subtreeNode(ctx, child, apiNode.Path, childDepth, opts))
	}
	return out
}

func (r *Routes) subtreeLinkCounts(ctx context.Context, node *tree.PageNode) any {
	if r == nil || r.linkStatus == nil || node == nil || node.Kind != tree.NodeKindPage {
		return nil
	}
	out, err := r.linkStatus.Execute(ctx, wikilinks.GetLinkStatusInput{PageID: node.ID})
	if err != nil || out == nil || out.Status == nil {
		return nil
	}
	return out.Status.Counts
}

func (r *Routes) subtreeContentPreview(pageID string) string {
	if r == nil || r.treeService == nil {
		return ""
	}
	page, err := r.treeService.GetPage(pageID)
	if err != nil {
		return ""
	}
	preview := strings.Join(strings.Fields(page.Content), " ")
	if len(preview) > 240 {
		return preview[:240]
	}
	return preview
}

func parentPathForNode(node *tree.PageNode) string {
	if node == nil || node.Parent == nil || node.Parent.Slug == "root" {
		return ""
	}
	return strings.Trim(node.Parent.CalculatePath(), "/")
}

func (r *Routes) breadcrumbNodes(ctx context.Context, node *tree.PageNode, opts subtreeOptions) []*subtreeNode {
	stack := []*tree.PageNode{}
	for current := node; current != nil; current = current.Parent {
		stack = append([]*tree.PageNode{current}, stack...)
	}
	out := make([]*subtreeNode, 0, len(stack))
	for _, item := range stack {
		out = append(out, r.subtreeNode(ctx, item, parentPathForNode(item), 0, opts))
	}
	return out
}

func ensureSubtreeNodeChildrenArray(node *subtreeNode) {
	if node == nil {
		return
	}
	if node.Children == nil {
		node.Children = []*subtreeNode{}
	}
	for _, child := range node.Children {
		ensureSubtreeNodeChildrenArray(child)
	}
}

func subtreeTruncated(node *tree.PageNode, depth int) bool {
	if node == nil || depth < 0 {
		return false
	}
	if depth == 0 {
		return len(node.Children) > 0
	}
	for _, child := range node.Children {
		if subtreeTruncated(child, depth-1) {
			return true
		}
	}
	return false
}

func boundedSubtreeDepth(raw *int) (int, error) {
	if raw == nil {
		return defaultContextTreeDepth, nil
	}
	if *raw < 0 {
		return 0, fmt.Errorf("depth must be zero or greater")
	}
	if *raw > maxContextTreeDepth {
		return maxContextTreeDepth, nil
	}
	return *raw, nil
}

package pages

import "github.com/perber/wiki/internal/core/tree"

func sanitizeSemanticClientVersion(v tree.PageVersion) tree.PageVersion {
	if v.IsUnchecked() {
		return ""
	}
	return v
}

func pageAssetURLPrefix(id tree.PageID) string {
	return "/assets/" + id.MetadataValue() + "/"
}

// collectSubtreeIDs returns all page IDs within a subtree (excluding "root").
func collectSubtreeIDs(node *tree.PageNode) []tree.PageID {
	var ids []tree.PageID
	var walk func(n *tree.PageNode)
	walk = func(n *tree.PageNode) {
		if n == nil {
			return
		}
		if n.ID != "root" {
			ids = append(ids, n.ID)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(node)
	return ids
}

package pages

import "github.com/perber/wiki/internal/core/tree"

// sanitizeClientVersion rejects the internal VersionUnchecked sentinel so
// external callers cannot bypass optimistic locking by sending the sentinel value.
// Treated as "no version provided" — produces ErrVersionRequired for versioned nodes.
func sanitizeClientVersion(v string) string {
	if v == tree.VersionUnchecked {
		return ""
	}
	return v
}

// collectSubtreeIDs returns all page IDs within a subtree (excluding "root").
func collectSubtreeIDs(node *tree.PageNode) []string {
	var ids []string
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

package links

import "github.com/perber/wiki/internal/core/tree"

type RefactorLinkMatch struct {
	FromPageID tree.PageID
	FromTitle  string
	ToPath     tree.RoutePath
	ToKind     string
	Broken     bool
}

type RewriteRule struct {
	OldPath    tree.RoutePath
	NewPath    tree.RoutePath
	Kind       string
	OutputKind string
}

package links

import "github.com/perber/wiki/internal/core/tree"

type Outgoing struct {
	FromPageID tree.PageID
	ToPageID   tree.PageID
	FromTitle  string
	ToPath     tree.RoutePath // Path of the target page
	ToKind     TargetKind
	Broken     bool // Indicates if the link is broken
}

type OutgoingResult struct {
	Outgoings []OutgoingResultItem `json:"outgoings"`
	Count     int                  `json:"count"`
}

type OutgoingResultItem struct {
	ToPageID    tree.PageID    `json:"to_page_id"`
	ToPageTitle string         `json:"to_page_title"`
	ToPath      tree.RoutePath `json:"to_path"`
	ToKind      TargetKind     `json:"to_kind"`
	Broken      bool           `json:"broken"`
	FromPageID  tree.PageID    `json:"from_page_id"`
}

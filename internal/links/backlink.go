package links

import "github.com/perber/wiki/internal/core/tree"

type Backlink struct {
	FromPageID tree.PageID
	ToPageID   tree.PageID
	FromTitle  string
	ToKind     TargetKind
	Broken     bool
}

type BacklinkResult struct {
	Backlinks []BacklinkResultItem `json:"backlinks"`
	Count     int                  `json:"count"`
}

type BacklinkResultItem struct {
	FromPageID tree.PageID   `json:"from_page_id"`
	FromTitle  string        `json:"from_title"`
	FromPath   string        `json:"from_path"`
	FromKind   tree.NodeKind `json:"from_kind"`
	Broken     bool          `json:"broken"`
	ToPageID   tree.PageID   `json:"to_page_id"`
}

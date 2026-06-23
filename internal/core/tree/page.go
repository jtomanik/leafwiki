package tree

type Page struct {
	*PageNode
	Content    string `json:"content"`
	RawContent string `json:"-"` // full file including metadata; never serialised
}

type PermalinkTarget struct {
	ID   PageID   `json:"id"`
	Slug Slug     `json:"slug"`
	Path string   `json:"path"`
	Kind NodeKind `json:"kind"`
}

// Version returns a stable optimistic-lock token for the current page state.
func (p *Page) Version() PageVersion {
	if p == nil || p.PageNode == nil {
		return ""
	}
	return p.PageNode.Version()
}

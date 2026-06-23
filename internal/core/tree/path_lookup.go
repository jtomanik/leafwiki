package tree

// PathLookup helpers for LookupPath()
type PathSegment struct {
	Slug   Slug      `json:"slug"`
	Exists bool      `json:"exists"`
	Kind   *NodeKind `json:"kind,omitempty"`
	Title  *string   `json:"title,omitempty"`
	ID     *PageID   `json:"id,omitempty"`
}

type PathLookup struct {
	Path      RoutePath     `json:"path"`
	Segments  []PathSegment `json:"segments"`
	Exists    bool          `json:"exists"`
	CanCreate bool          `json:"canCreate"`
}

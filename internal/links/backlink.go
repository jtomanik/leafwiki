package links

type Backlink struct {
	FromPageID string
	ToPageID   string
	FromTitle  string
	ToKind     string
	Broken     bool
}

type BacklinkResult struct {
	Backlinks []BacklinkResultItem `json:"backlinks"`
	Count     int                  `json:"count"`
}

type BacklinkResultItem struct {
	FromPageID string `json:"from_page_id"`
	FromTitle  string `json:"from_title"`
	FromPath   string `json:"from_path"`
	FromKind   string `json:"from_kind"`
	Broken     bool   `json:"broken"`
	ToPageID   string `json:"to_page_id"`
}

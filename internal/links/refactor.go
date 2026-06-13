package links

type RefactorLinkMatch struct {
	FromPageID string
	FromTitle  string
	ToPath     string
	ToKind     string
	Broken     bool
}

type RewriteRule struct {
	OldPath    string
	NewPath    string
	Kind       string
	OutputKind string
}

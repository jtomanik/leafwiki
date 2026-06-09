package tree

import "testing"

func TestMarkdownPathToRoutePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "root index", path: "index.md", want: ""},
		{name: "root uppercase index", path: "INDEX.MD", want: ""},
		{name: "section index", path: "docs/index.md", want: "docs"},
		{name: "section uppercase index", path: "docs/INDEX.MD", want: "docs"},
		{name: "regular page", path: "docs/guide.md", want: "docs/guide"},
		{name: "leading slash", path: "/docs/guide.md", want: "docs/guide"},
		{name: "already route-like", path: "docs/guide", want: "docs/guide"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MarkdownPathToRoutePath(tt.path); got != tt.want {
				t.Fatalf("MarkdownPathToRoutePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

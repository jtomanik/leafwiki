package dto

import (
	"errors"
	"testing"
	"time"

	"github.com/perber/wiki/internal/core/tree"
)

func TestToAPINodeWithContentPaths_OmitsContentPathWhenResolverFails(t *testing.T) {
	node := &tree.PageNode{
		ID:       "docs",
		Title:    "Docs",
		Slug:     "docs",
		Kind:     tree.NodeKindSection,
		Metadata: tree.PageMetadata{CreatedAt: time.Now(), UpdatedAt: time.Now()},
	}

	apiNode := ToAPINodeWithContentPaths(node, "", nil, func(*tree.PageNode) (string, error) {
		return "", errors.New("resolver failed")
	})

	if apiNode.ContentPath != "" {
		t.Fatalf("ContentPath = %q, want empty when resolver fails", apiNode.ContentPath)
	}
}

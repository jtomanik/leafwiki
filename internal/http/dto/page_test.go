package dto

import (
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/core/tree"
	coreprop "github.com/perber/wiki/internal/properties"
)

func matchAPIPage(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

func matchAPINode(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

func matchAPINodeID(expected tree.PageID) types.GomegaMatcher {
	GinkgoHelper()
	return WithTransform(func(raw string) tree.PageID {
		return tree.PageIDFromString(raw)
	}, Equal(expected))
}

func matchPropertyPage(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

func matchTaggedPage(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

var _ = Describe("page DTO mapping", func() {
	It("maps a full page with metadata, content, path, and initialized collections", func() {
		root, child, _ := dtoTestTree()
		resolver := newDTOUserResolver(root, child)

		page := ToAPIPage(&tree.Page{PageNode: child, Content: "# Intro"}, resolver)

		Expect(page).To(matchAPIPage(gstruct.Fields{
			"Content":    Equal("# Intro"),
			"Path":       Equal("docs/intro"),
			"Tags":       SatisfyAll(BeEmpty(), Not(BeNil())),
			"Properties": SatisfyAll(BeEmpty(), Not(BeNil())),
			"Node": matchAPINode(gstruct.Fields{
				"ID":   Equal("intro"),
				"Path": Equal("docs/intro"),
				"Metadata": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"Creator":    Equal(&coreauth.UserLabel{ID: root.Metadata.CreatorID.String(), Username: "creator"}),
					"LastAuthor": Equal(&coreauth.UserLabel{ID: root.Metadata.LastAuthorID.String(), Username: "last-author"}),
				}),
			}),
		}))
	})

	It("prunes page children when converting a page with depth zero", func() {
		root, _, _ := dtoTestTree()

		page := ToAPIPageWithDepth(&tree.Page{PageNode: root, Content: "# Docs"}, nil, 0)

		Expect(page.Children).To(BeEmpty())
	})

	It("maps node children, content paths, and README fallback state", func() {
		root, child, _ := dtoTestTree()

		apiNode := ToAPINodeWithContentPaths(root, "", nil, func(node *tree.PageNode) (string, error) {
			if node == root {
				return "docs/README.md", nil
			}
			return "docs/" + node.Slug.FilesystemPath() + ".md", nil
		})

		Expect(apiNode).To(matchAPINode(gstruct.Fields{
			"Path":           Equal("docs"),
			"ContentPath":    Equal("docs/README.md"),
			"ReadmeFallback": BeTrue(),
			"Children": ConsistOf(matchAPINode(gstruct.Fields{
				"ID":             matchAPINodeID(child.ID),
				"ContentPath":    Equal("docs/intro.md"),
				"ReadmeFallback": BeFalse(),
			})),
		}))
	})

	It("omits content paths when the resolver fails", func() {
		root, _, _ := dtoTestTree()

		apiNode := ToAPINodeWithContentPaths(root, "", nil, func(*tree.PageNode) (string, error) {
			return "", errors.New("resolver failed")
		})

		Expect(apiNode.ContentPath).To(BeEmpty())
	})

	It("applies node depth limits and allows unlimited depth", func() {
		root, child, grandchild := dtoTestTree()

		depthOne := ToAPINodeWithDepth(root, "", nil, 1)
		unlimited := ToAPINodeWithDepth(root, "", nil, -1)
		withContentPathDepthZero := ToAPINodeWithContentPathsAndDepth(root, "", nil, func(node *tree.PageNode) (string, error) {
			return node.CalculateRoutePath().FilesystemPath() + ".md", nil
		}, 0)
		withContentPathUnlimited := ToAPINodeWithContentPathsAndDepth(root, "", nil, nil, -1)

		Expect(depthOne).To(matchAPINode(gstruct.Fields{
			"Children": ConsistOf(matchAPINode(gstruct.Fields{
				"Children": BeEmpty(),
			})),
		}))
		Expect(unlimited).To(matchAPINode(gstruct.Fields{
			"Children": ConsistOf(matchAPINode(gstruct.Fields{
				"Children": ConsistOf(matchAPINode(gstruct.Fields{
					"ID": matchAPINodeID(grandchild.ID),
				})),
			})),
		}))
		Expect(withContentPathDepthZero).To(matchAPINode(gstruct.Fields{
			"Children":    BeEmpty(),
			"ContentPath": Equal("docs.md"),
		}))
		Expect(withContentPathUnlimited).To(matchAPINode(gstruct.Fields{
			"Children": ConsistOf(matchAPINode(gstruct.Fields{
				"ID": matchAPINodeID(child.ID),
			})),
		}))
	})

	It("handles nil and unlimited pruning defensively", func() {
		root, _, _ := dtoTestTree()
		apiNode := ToAPINode(root, "", nil)

		Expect(func() {
			pruneNodeDepth(nil, 0)
			pruneNodeDepth(apiNode, -1)
		}).NotTo(Panic())
		Expect(apiNode.Children).To(HaveLen(1))
	})

	It("formats API times with empty zero values", func() {
		updated := time.Date(2026, 6, 26, 12, 34, 56, 0, time.UTC)

		Expect(FormatAPITime(time.Time{})).To(BeEmpty())
		Expect(FormatAPITime(updated)).To(Equal("2026-06-26T12:34:56Z"))
	})
})

var _ = Describe("property and tag DTO mapping", func() {
	It("maps property pages with copied properties and author labels", func() {
		root, child, _ := dtoTestTree()
		resolver := newDTOUserResolver(root, child)

		page := ToPropertyPage(child, map[string]coreprop.PropertyEntry{
			"status": {Value: "draft", Type: "text"},
		}, resolver)

		Expect(page).To(matchPropertyPage(gstruct.Fields{
			"ID":    Equal("intro"),
			"Title": Equal("Intro"),
			"Path":  Equal("docs/intro"),
			"Properties": Equal(map[string]PropertyEntry{
				"status": {Value: "draft", Type: "text"},
			}),
			"CreatedAt":  Equal("2026-06-26T10:00:00Z"),
			"UpdatedAt":  Equal("2026-06-26T11:00:00Z"),
			"LastAuthor": Equal(&coreauth.UserLabel{ID: root.Metadata.LastAuthorID.String(), Username: "last-author"}),
		}))
	})

	It("leaves optional property timestamps empty when metadata times are zero", func() {
		node := &tree.PageNode{ID: "untimed", Title: "Untimed", Slug: "untimed", Kind: tree.NodeKindPage}

		page := ToPropertyPage(node, nil, nil)

		Expect(page).To(matchPropertyPage(gstruct.Fields{
			"CreatedAt":  BeEmpty(),
			"UpdatedAt":  BeEmpty(),
			"Properties": BeEmpty(),
		}))
	})

	It("maps tagged pages and normalizes nil tags to an empty slice", func() {
		root, child, _ := dtoTestTree()
		resolver := newDTOUserResolver(root, child)

		tagged := ToTaggedPage(child, []string{"go", "wiki"}, "Intro excerpt", resolver)
		emptyTags := ToTaggedPage(root, nil, "", nil)

		Expect(tagged).To(matchTaggedPage(gstruct.Fields{
			"ID":         Equal("intro"),
			"Kind":       Equal(tree.NodeKindPage),
			"Path":       Equal("docs/intro"),
			"Excerpt":    Equal("Intro excerpt"),
			"Tags":       Equal([]string{"go", "wiki"}),
			"CreatedAt":  Equal("2026-06-26T10:00:00Z"),
			"UpdatedAt":  Equal("2026-06-26T11:00:00Z"),
			"LastAuthor": Equal(&coreauth.UserLabel{ID: root.Metadata.LastAuthorID.String(), Username: "last-author"}),
		}))
		Expect(emptyTags).To(matchTaggedPage(gstruct.Fields{
			"Tags": SatisfyAll(BeEmpty(), Not(BeNil())),
		}))
	})
})

func dtoTestTree() (*tree.PageNode, *tree.PageNode, *tree.PageNode) {
	created := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 6, 26, 11, 0, 0, 0, time.UTC)
	root := &tree.PageNode{
		ID:       "docs",
		Title:    "Docs",
		Slug:     "docs",
		Kind:     tree.NodeKindSection,
		Position: 1,
		Metadata: tree.PageMetadata{
			CreatedAt:    created,
			UpdatedAt:    updated,
			CreatorID:    tree.UserIDFromString("creator-id"),
			LastAuthorID: tree.UserIDFromString("last-author-id"),
		},
	}
	child := &tree.PageNode{
		ID:       "intro",
		Title:    "Intro",
		Slug:     "intro",
		Kind:     tree.NodeKindPage,
		Position: 2,
		Parent:   root,
		Metadata: root.Metadata,
	}
	grandchild := &tree.PageNode{
		ID:       "deep",
		Title:    "Deep",
		Slug:     "deep",
		Kind:     tree.NodeKindPage,
		Position: 3,
		Parent:   child,
		Metadata: root.Metadata,
	}
	root.Children = []*tree.PageNode{child}
	child.Children = []*tree.PageNode{grandchild}
	return root, child, grandchild
}

func newDTOUserResolver(nodes ...*tree.PageNode) *coreauth.UserResolver {
	GinkgoHelper()
	store, err := coreauth.NewUserStore(GinkgoT().TempDir())
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
	service := coreauth.NewUserService(store)
	creator, err := service.CreateUser("creator", "creator@example.com", "password", coreauth.RoleViewer)
	Expect(err).NotTo(HaveOccurred())
	lastAuthor, err := service.CreateUser("last-author", "last-author@example.com", "password", coreauth.RoleEditor)
	Expect(err).NotTo(HaveOccurred())
	for _, node := range nodes {
		node.Metadata.CreatorID = tree.UserIDFromString(creator.ID)
		node.Metadata.LastAuthorID = tree.UserIDFromString(lastAuthor.ID)
	}
	resolver, err := coreauth.NewUserResolver(service)
	Expect(err).NotTo(HaveOccurred())
	return resolver
}

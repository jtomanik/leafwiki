package properties

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/tree"
)

type propertyExtractionCase struct {
	content string
	matcher types.GomegaMatcher
}

func setupPropertiesService() (*PropertiesService, *tree.TreeService) {
	ginkgo.GinkgoHelper()

	storageDir := propertiesTempDir()
	treeService := tree.NewTreeService(storageDir)
	Expect(treeService.LoadTree()).To(Succeed())

	store, err := NewPropertiesStore(storageDir)
	Expect(err).To(Succeed())
	closeStoreForTest(store)

	return NewPropertiesService(store), treeService
}

func indexAllPages(svc *PropertiesService, treeService *tree.TreeService) {
	ginkgo.GinkgoHelper()

	var ids []tree.PageID
	Expect(treeService.WalkNodes(func(id tree.PageID) error {
		ids = append(ids, id)
		return nil
	})).To(Succeed())

	pages, errs := treeService.GetPages(ids)
	Expect(errs).To(HaveEach(Succeed()))
	for _, page := range pages {
		Expect(svc.IndexPageContent(page.ID, page.RawContent)).To(Succeed())
	}
}

func pageKind() *tree.NodeKind {
	kind := tree.NodeKindPage
	return &kind
}

func createPageWithContent(treeService *tree.TreeService, title, slug, content string) tree.PageID {
	ginkgo.GinkgoHelper()

	id, err := treeService.CreateNode(newFixtureUserID("system"), nil, title, tree.SlugFromString(slug), pageKind())
	Expect(err).To(Succeed())
	Expect(id).NotTo(BeNil())
	Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("system"), *id, title, tree.SlugFromString(slug), &content, true)).To(Succeed())
	return *id
}

var _ = ginkgo.Describe("property extraction from page metadata", func() {
	ginkgo.DescribeTable("indexes text metadata fields",
		func(row propertyExtractionCase) {
			Expect(ExtractPropertiesFromContent(row.content)).To(row.matcher)
		},
		ginkgo.Entry("stores a single text value", propertyExtractionCase{
			content: "---\nstatus: draft\n---\n",
			matcher: matchExtractedProperties("status", "draft"),
		}),
		ginkgo.Entry("stores multiple text values", propertyExtractionCase{
			content: "---\nstatus: draft\nauthor: alice\nenvironment: staging\n---\n",
			matcher: matchExtractedProperties("status", "draft", "author", "alice", "environment", "staging"),
		}),
		ginkgo.Entry("trims text value whitespace", propertyExtractionCase{
			content: "---\nstatus: \"  draft  \"\n---\n",
			matcher: matchExtractedProperties("status", "draft"),
		}),
	)

	ginkgo.DescribeTable("ignores metadata that is not an indexable text property",
		func(row propertyExtractionCase) {
			Expect(ExtractPropertiesFromContent(row.content)).To(row.matcher)
		},
		ginkgo.Entry("skips number values while preserving text", propertyExtractionCase{
			content: "---\nscore: 42\nrating: 4.5\nstatus: draft\n---\n",
			matcher: matchExtractedProperties("status", "draft"),
		}),
		ginkgo.Entry("skips boolean values while preserving text", propertyExtractionCase{
			content: "---\nfeatured: true\narchived: false\nstatus: draft\n---\n",
			matcher: matchExtractedProperties("status", "draft"),
		}),
		ginkgo.Entry("skips empty strings while preserving text", propertyExtractionCase{
			content: "---\nempty: \"\"\nstatus: draft\n---\n",
			matcher: matchExtractedProperties("status", "draft"),
		}),
		ginkgo.Entry("skips whitespace-only strings while preserving text", propertyExtractionCase{
			content: "---\nblank: \"   \"\nstatus: draft\n---\n",
			matcher: matchExtractedProperties("status", "draft"),
		}),
		ginkgo.Entry("skips multiline strings while preserving text", propertyExtractionCase{
			content: "---\ndescription: |\n  line one\n  line two\nstatus: draft\n---\n",
			matcher: matchExtractedProperties("status", "draft"),
		}),
		ginkgo.Entry("skips inline lists while preserving text", propertyExtractionCase{
			content: "---\nkeywords: [go, testing]\nstatus: draft\n---\n",
			matcher: matchExtractedProperties("status", "draft"),
		}),
		ginkgo.Entry("skips block lists while preserving text", propertyExtractionCase{
			content: "---\nkeywords:\n  - go\n  - testing\nstatus: draft\n---\n",
			matcher: matchExtractedProperties("status", "draft"),
		}),
		ginkgo.Entry("returns no properties when values are nil", propertyExtractionCase{
			content: "---\nempty:\nstatus:\n---\n",
			matcher: BeNil(),
		}),
		ginkgo.Entry("returns no properties when every value is non-text", propertyExtractionCase{
			content: "---\nscore: 42\nfeatured: true\nrating: 4.5\n---\n",
			matcher: BeNil(),
		}),
	)

	ginkgo.DescribeTable("ignores reserved metadata fields",
		func(row propertyExtractionCase) {
			Expect(ExtractPropertiesFromContent(row.content)).To(row.matcher)
		},
		ginkgo.Entry("skips tags", propertyExtractionCase{
			content: "---\ntags:\n  - react\nstatus: draft\n---\n",
			matcher: matchExtractedProperties("status", "draft"),
		}),
		ginkgo.Entry("skips tags case-insensitively", propertyExtractionCase{
			content: "---\nTags:\n  - react\nstatus: draft\n---\n",
			matcher: matchExtractedProperties("status", "draft"),
		}),
		ginkgo.Entry("skips title", propertyExtractionCase{
			content: "---\ntitle: My Page\nstatus: draft\n---\n",
			matcher: matchExtractedProperties("status", "draft"),
		}),
		ginkgo.Entry("skips title case-insensitively", propertyExtractionCase{
			content: "---\nTitle: My Page\nstatus: draft\n---\n",
			matcher: matchExtractedProperties("status", "draft"),
		}),
		ginkgo.Entry("skips LeafWiki managed fields", propertyExtractionCase{
			content: "---\nleafwiki_id: abc123\nleafwiki_created: 2024-01-01\nstatus: draft\n---\n",
			matcher: matchExtractedProperties("status", "draft"),
		}),
		ginkgo.Entry("skips LeafWiki managed fields case-insensitively", propertyExtractionCase{
			content: "---\nLeafwiki_ID: abc\nstatus: ok\n---\n",
			matcher: matchExtractedProperties("status", "ok"),
		}),
		ginkgo.Entry("returns no properties when every key is reserved", propertyExtractionCase{
			content: "---\ntags:\n  - go\ntitle: My Page\nleafwiki_id: abc\n---\n",
			matcher: BeNil(),
		}),
	)

	ginkgo.DescribeTable("returns no properties when metadata is absent",
		func(content string) {
			Expect(ExtractPropertiesFromContent(content)).To(BeNil())
		},
		ginkgo.Entry("plain markdown has no frontmatter", "# Page\n\nNo frontmatter."),
		ginkgo.Entry("empty content has no frontmatter", ""),
	)
})

var _ = ginkgo.Describe("properties service page indexing", func() {
	ginkgo.When("all tree pages are indexed", func() {
		ginkgo.It("builds a queryable property index from page frontmatter", func() {
			svc, treeService := setupPropertiesService()
			draftID := createPageWithContent(treeService, "Page A", "page-a", "---\nstatus: draft\n---\n# A")
			publishedID := createPageWithContent(treeService, "Page B", "page-b", "---\nstatus: published\n---\n# B")

			indexAllPages(svc, treeService)

			draftIDs, err := svc.GetPageIDsByProperty("status", "draft")
			Expect(err).To(Succeed())
			Expect(draftIDs).To(Equal([]tree.PageID{draftID}))

			publishedIDs, err := svc.GetPageIDsByProperty("status", "published")
			Expect(err).To(Succeed())
			Expect(publishedIDs).To(Equal([]tree.PageID{publishedID}))
		})

		ginkgo.It("can rebuild the index repeatedly without duplicating keys", func() {
			svc, treeService := setupPropertiesService()
			createPageWithContent(treeService, "Page A", "page-a", "---\nstatus: draft\n---\n# A")

			for range 3 {
				Expect(svc.ClearIndex()).To(Succeed())
				indexAllPages(svc, treeService)
			}

			keys, err := svc.GetAllPropertyKeys("", 50)
			Expect(err).To(Succeed())
			Expect(keys).To(havePropertyKeys(PropertyKeyCount{Key: "status", Count: 1}))
		})

		ginkgo.It("keeps reserved keys and list values out of the index", func() {
			svc, treeService := setupPropertiesService()
			createPageWithContent(treeService, "Page A", "page-a",
				"---\ntags:\n  - go\ntitle: Custom\nleafwiki_id: abc\nkeywords: [go, testing]\nstatus: draft\n---\n# A")

			indexAllPages(svc, treeService)

			keys, err := svc.GetAllPropertyKeys("", 50)
			Expect(err).To(Succeed())
			Expect(keys).To(havePropertyKeys(PropertyKeyCount{Key: "status", Count: 1}))
		})

		ginkgo.It("skips pages without indexable properties", func() {
			svc, treeService := setupPropertiesService()
			createPageWithContent(treeService, "No Props", "no-props", "# Just content")

			indexAllPages(svc, treeService)

			keys, err := svc.GetAllPropertyKeys("", 50)
			Expect(err).To(Succeed())
			Expect(keys).To(BeEmpty())
		})

		ginkgo.It("indexes raw frontmatter even when parsed page content omits it", func() {
			svc, treeService := setupPropertiesService()
			pageID := createPageWithContent(treeService, "Page A", "page-a", "---\nstatus: draft\n---\n# A")

			page, err := treeService.GetPage(pageID)
			Expect(err).To(Succeed())
			Expect(ExtractPropertiesFromContent(page.Content)).To(BeNil())

			indexAllPages(svc, treeService)

			ids, err := svc.GetPageIDsByProperty("status", "draft")
			Expect(err).To(Succeed())
			Expect(ids).To(Equal([]tree.PageID{pageID}))
		})
	})

	ginkgo.When("one page is indexed directly", func() {
		ginkgo.It("stores text properties from raw frontmatter", func() {
			svc, _ := setupPropertiesService()

			Expect(svc.IndexPageContent(newFixturePageID("page-1"), "---\nstatus: draft\nauthor: alice\n---\n\n# Page")).To(Succeed())

			keys, err := svc.GetAllPropertyKeys("", 50)
			Expect(err).To(Succeed())
			Expect(keys).To(ConsistOf(
				PropertyKeyCount{Key: "status", Count: 1},
				PropertyKeyCount{Key: "author", Count: 1},
			))

			ids, err := svc.GetPageIDsByProperty("status", "draft")
			Expect(err).To(Succeed())
			Expect(ids).To(Equal(testPageIDs("page-1")))
		})

		ginkgo.It("stores nothing when the page has no frontmatter", func() {
			svc, _ := setupPropertiesService()

			Expect(svc.IndexPageContent(newFixturePageID("page-1"), "# Just content")).To(Succeed())

			keys, err := svc.GetAllPropertyKeys("", 50)
			Expect(err).To(Succeed())
			Expect(keys).To(BeEmpty())
		})

		ginkgo.It("replaces old property values for the page", func() {
			svc, _ := setupPropertiesService()

			Expect(svc.IndexPageContent(newFixturePageID("page-1"), "---\nstatus: draft\n---\n")).To(Succeed())
			Expect(svc.IndexPageContent(newFixturePageID("page-1"), "---\nstatus: published\n---\n")).To(Succeed())

			publishedIDs, err := svc.GetPageIDsByProperty("status", "published")
			Expect(err).To(Succeed())
			Expect(publishedIDs).To(Equal(testPageIDs("page-1")))

			draftIDs, err := svc.GetPageIDsByProperty("status", "draft")
			Expect(err).To(Succeed())
			Expect(draftIDs).To(BeEmpty())
		})
	})

	ginkgo.When("the service delegates explicit store operations", func() {
		ginkgo.It("sets and returns properties for selected pages", func() {
			store := newTestStore()
			svc := NewPropertiesService(store)

			Expect(svc.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft", "owner", "alice"))).To(Succeed())
			Expect(svc.SetPropertiesForPage(newFixturePageID("page-2"), props("status", "published"))).To(Succeed())

			got, err := svc.GetPropertiesForPages(testPageIDs("page-1", "page-2"))
			Expect(err).To(Succeed())
			Expect(got).To(Equal(map[tree.PageID]map[string]PropertyEntry{
				newFixturePageID("page-1"): props("owner", "alice", "status", "draft"),
				newFixturePageID("page-2"): props("status", "published"),
			}))
		})

		ginkgo.It("deletes one page without removing other page properties", func() {
			store := newTestStore()
			svc := NewPropertiesService(store)

			Expect(svc.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))).To(Succeed())
			Expect(svc.SetPropertiesForPage(newFixturePageID("page-2"), props("status", "published"))).To(Succeed())
			Expect(svc.DeletePropertiesForPage(newFixturePageID("page-1"))).To(Succeed())

			got, err := svc.GetPropertiesForPages(testPageIDs("page-1", "page-2"))
			Expect(err).To(Succeed())
			Expect(got).NotTo(HaveKey(newFixturePageID("page-1")))
			Expect(got).To(HaveKeyWithValue(newFixturePageID("page-2"), props("status", "published")))
		})

		ginkgo.It("returns an empty properties map for an empty selection", func() {
			store := newTestStore()
			svc := NewPropertiesService(store)

			got, err := svc.GetPropertiesForPages(nil)

			Expect(err).To(Succeed())
			Expect(got).To(BeEmpty())
		})
	})
})

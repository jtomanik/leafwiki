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

func createPageWithContent(treeService *tree.TreeService, title string, slug tree.Slug, content string) tree.PageID {
	ginkgo.GinkgoHelper()

	id, err := treeService.CreateNode(newFixtureUserID("system"), nil, title, slug, pageKind())
	Expect(err).To(Succeed())
	Expect(id).NotTo(BeNil())
	Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("system"), *id, title, slug, &content, true)).To(Succeed())
	return *id
}

type fakePropertiesStore struct {
	clears             int
	setProperties      map[tree.PageID]map[string]PropertyEntry
	deletedPages       []tree.PageID
	keyFilter          string
	keyLimit           PropertyKeyLimit
	propertyLookup     propertyLookupRequest
	propertiesRequests [][]tree.PageID
	keys               []PropertyKeyCount
	matchingPages      []tree.PageID
	propertiesByPage   map[tree.PageID]map[string]PropertyEntry
}

type propertyLookupRequest struct {
	Key   string
	Value string
}

type propertiesServiceStoreObservation struct {
	Clears             int
	SetProperties      map[tree.PageID]map[string]PropertyEntry
	DeletedPages       []tree.PageID
	KeyFilter          string
	KeyLimit           PropertyKeyLimit
	PropertyLookup     propertyLookupRequest
	PropertiesRequests [][]tree.PageID
}

func newFakePropertiesStore() *fakePropertiesStore {
	return &fakePropertiesStore{
		setProperties: map[tree.PageID]map[string]PropertyEntry{},
		keys: []PropertyKeyCount{
			{Key: "owner", Count: 1},
			{Key: "status", Count: 2},
		},
		matchingPages: testPageIDs("page-1", "page-2"),
		propertiesByPage: map[tree.PageID]map[string]PropertyEntry{
			newFixturePageID("page-1"): props("status", "draft"),
			newFixturePageID("page-2"): props("status", "published"),
		},
	}
}

func (s *fakePropertiesStore) Clear() error {
	s.clears++
	return nil
}

func (s *fakePropertiesStore) SetPropertiesForPage(pageID tree.PageID, props map[string]PropertyEntry) error {
	s.setProperties[pageID] = props
	return nil
}

func (s *fakePropertiesStore) DeletePropertiesForPage(pageID tree.PageID) error {
	s.deletedPages = append(s.deletedPages, pageID)
	return nil
}

func (s *fakePropertiesStore) GetAllPropertyKeys(filter string, pageSize PropertyKeyLimit) ([]PropertyKeyCount, error) {
	s.keyFilter = filter
	s.keyLimit = pageSize
	return s.keys, nil
}

func (s *fakePropertiesStore) GetPageIDsByProperty(key, value string) ([]tree.PageID, error) {
	s.propertyLookup = propertyLookupRequest{Key: key, Value: value}
	return s.matchingPages, nil
}

func (s *fakePropertiesStore) GetPropertiesForPages(pageIDs []tree.PageID) (map[tree.PageID]map[string]PropertyEntry, error) {
	s.propertiesRequests = append(s.propertiesRequests, append([]tree.PageID(nil), pageIDs...))
	return s.propertiesByPage, nil
}

func observePropertiesServiceStore(store *fakePropertiesStore) propertiesServiceStoreObservation {
	return propertiesServiceStoreObservation{
		Clears:             store.clears,
		SetProperties:      store.setProperties,
		DeletedPages:       store.deletedPages,
		KeyFilter:          store.keyFilter,
		KeyLimit:           store.keyLimit,
		PropertyLookup:     store.propertyLookup,
		PropertiesRequests: store.propertiesRequests,
	}
}

var _ = ginkgo.Describe("property extraction from page metadata", ginkgo.Label("unit"), func() {
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

var _ = ginkgo.Describe("properties service store boundary", ginkgo.Label("unit"), func() {
	ginkgo.It("delegates page-property operations through the package store contract", func() {
		store := newFakePropertiesStore()
		svc := NewPropertiesService(store)

		Expect(svc.ClearIndex()).To(Succeed())
		Expect(svc.SetPropertiesForPage(newFixturePageID("page-1"), props("status", "draft"))).To(Succeed())
		Expect(svc.IndexPageContent(newFixturePageID("page-3"), "---\nstatus: review\n---\n# Draft")).To(Succeed())
		Expect(svc.DeletePropertiesForPage(newFixturePageID("page-2"))).To(Succeed())

		keys, err := svc.GetAllPropertyKeys("st", 10)
		Expect(err).To(Succeed())
		Expect(keys).To(Equal([]PropertyKeyCount{
			{Key: "owner", Count: 1},
			{Key: "status", Count: 2},
		}))

		pageIDs, err := svc.GetPageIDsByProperty("status", "draft")
		Expect(err).To(Succeed())
		Expect(pageIDs).To(Equal(testPageIDs("page-1", "page-2")))

		propertiesByPage, err := svc.GetPropertiesForPages(testPageIDs("page-1", "page-2"))
		Expect(err).To(Succeed())
		Expect(propertiesByPage).To(Equal(map[tree.PageID]map[string]PropertyEntry{
			newFixturePageID("page-1"): props("status", "draft"),
			newFixturePageID("page-2"): props("status", "published"),
		}))

		Expect(observePropertiesServiceStore(store)).To(Equal(propertiesServiceStoreObservation{
			Clears: 1,
			SetProperties: map[tree.PageID]map[string]PropertyEntry{
				newFixturePageID("page-1"): props("status", "draft"),
				newFixturePageID("page-3"): props("status", "review"),
			},
			DeletedPages:   testPageIDs("page-2"),
			KeyFilter:      "st",
			KeyLimit:       10,
			PropertyLookup: propertyLookupRequest{Key: "status", Value: "draft"},
			PropertiesRequests: [][]tree.PageID{
				testPageIDs("page-1", "page-2"),
			},
		}))
	})
})

var _ = ginkgo.Describe("properties service page indexing", ginkgo.Label("integration"), func() {
	ginkgo.When("all tree pages are indexed", func() {
		ginkgo.It("builds a queryable property index from page frontmatter", func() {
			svc, treeService := setupPropertiesService()
			draftID := createPageWithContent(treeService, "Page A", newFixtureSlug("page-a"), "---\nstatus: draft\n---\n# A")
			publishedID := createPageWithContent(treeService, "Page B", newFixtureSlug("page-b"), "---\nstatus: published\n---\n# B")

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
			createPageWithContent(treeService, "Page A", newFixtureSlug("page-a"), "---\nstatus: draft\n---\n# A")

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
			createPageWithContent(treeService, "Page A", newFixtureSlug("page-a"),
				"---\ntags:\n  - go\ntitle: Custom\nleafwiki_id: abc\nkeywords: [go, testing]\nstatus: draft\n---\n# A")

			indexAllPages(svc, treeService)

			keys, err := svc.GetAllPropertyKeys("", 50)
			Expect(err).To(Succeed())
			Expect(keys).To(havePropertyKeys(PropertyKeyCount{Key: "status", Count: 1}))
		})

		ginkgo.It("skips pages without indexable properties", func() {
			svc, treeService := setupPropertiesService()
			createPageWithContent(treeService, "No Props", newFixtureSlug("no-props"), "# Just content")

			indexAllPages(svc, treeService)

			keys, err := svc.GetAllPropertyKeys("", 50)
			Expect(err).To(Succeed())
			Expect(keys).To(BeEmpty())
		})

		ginkgo.It("indexes raw frontmatter even when parsed page content omits it", func() {
			svc, treeService := setupPropertiesService()
			pageID := createPageWithContent(treeService, "Page A", newFixtureSlug("page-a"), "---\nstatus: draft\n---\n# A")

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

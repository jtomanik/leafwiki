package tree

import (
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("tree hash changes when order changes", func() {
		svc, _ := newLoadedService()

		firstID, err := svc.CreateNode(newFixtureUserID("system"), nil, "One", newFixtureSlug("one"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode first failed: %v",

			err)

		secondID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Two", newFixtureSlug("two"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode second failed: %v",

			err)

		before := svc.TreeHash()
		{
			err := svc.SortPages(newFixturePageID(""), testPageIDs(*secondID, *firstID))
			Expect(err).To(Succeed(), "SortPages failed: %v",

				err)
		}

		after := svc.TreeHash()
		Expect(before).NotTo(Equal(after), "expected hash to change after sort")

	})
})

// --- B) Create/Update/Delete disk sync ---

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node reloads from filesystem", func() {
		svc, tmpDir := newLoadedService()

		id, err := svc.CreateNode(newFixtureUserID("system"), nil, "Welcome", newFixtureSlug("welcome"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		reloaded := NewTreeService(tmpDir)
		{
			err := reloaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		root := reloaded.GetTree()
		Expect(root.Children).To(ConsistOf(HaveField("ID", Equal(*id))))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create child rolls back parent auto convert when tree save fails", func() {
		svc, tmpDir := newLoadedService()

		parentID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Docs", newFixtureSlug("docs"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode parent failed: %v",

			err)

		statTreePath(filepath.Join(tmpDir, "root", "docs.md"))

		createTreeDirectory(filepath.Join(tmpDir, "root", "docs", ".order.json"))

		childID, err := svc.CreateNode(newFixtureUserID("system"), parentID, "Child", newFixtureSlug("child"), ptrKind(NodeKindPage))
		Expect(err).To(MatchError(ErrPersistChildOrder), "expected child order persistence error, got: %v",

			err)

		Expect(childID).To(BeNil())

		root := svc.GetTree()
		Expect(root.Children).To(HaveLen(1),
			"expected only original parent after rollback, got %d root children",

			len(root.Children))

		parent := root.Children[0]
		Expect(parent).To(SatisfyAll(
			HaveField("Kind", Equal(NodeKindPage)),
			HaveField("Children", BeEmpty()),
		), "expected parent to roll back to a childless page, got %#v", parent)

		statTreePath(filepath.Join(tmpDir, "root", "docs.md"))
		Expect(filepath.Join(tmpDir, "root", "docs")).To(beMissingTreePath())
		Expect(filepath.Join(tmpDir, "root", "docs", "index.md")).To(beMissingTreePath())
		Expect(filepath.Join(tmpDir, "root", "docs", "child.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node rolls back when tree save fails", func() {
		svc, tmpDir := newLoadedService()

		createTreeDirectory(filepath.Join(tmpDir, "root", ".order.json"))

		id, err := svc.CreateNode(newFixtureUserID("system"), nil, "Welcome", newFixtureSlug("welcome"), ptrKind(NodeKindPage))
		Expect(err).To(MatchError(ErrPersistChildOrder), "expected child order persistence error, got: %v",

			err)

		Expect(id).To(BeNil())
		Expect(svc.GetTree().
			Children).
			To(HaveLen(0), "expected in-memory tree rollback, got %d root children",

				len(svc.GetTree().Children))

		Expect(filepath.Join(tmpDir, "root", "welcome.md")).To(beMissingTreePath())
		info, err := os.Stat(filepath.Join(tmpDir, "root", ".order.json"))
		Expect(err).To(Succeed())
		Expect(info).To(matchFileInfoKind(fileInfoDirectory))
		reloaded := NewTreeService(tmpDir)
		{
			err := reloaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}
		Expect(reloaded.GetTree().Children).To(HaveLen(0), "expected no persisted children after rollback, got %d",

			len(reloaded.GetTree().Children),
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node rolls back when order write fails", func() {
		svc, tmpDir := newLoadedService()

		createTreeDirectory(filepath.Join(tmpDir, "root", ".order.json"))

		id, err := svc.CreateNode(newFixtureUserID("system"), nil, "Welcome", newFixtureSlug("welcome"), ptrKind(NodeKindPage))
		Expect(err).To(MatchError(ErrPersistChildOrder), "expected child order persistence error, got: %v",

			err)

		Expect(id).To(BeNil())
		Expect(svc.GetTree().
			Children).
			To(HaveLen(0), "expected in-memory tree rollback, got %d root children",

				len(svc.GetTree().Children))

		Expect(filepath.Join(tmpDir, "root", "welcome.md")).To(beMissingTreePath())

		reloaded := NewTreeService(tmpDir)
		{
			err := reloaded.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}
		Expect(reloaded.GetTree().Children).To(HaveLen(0), "expected no persisted children after rollback, got %d",

			len(reloaded.GetTree().Children),
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node page root creates file and frontmatter", func() {
		svc, tmpDir := newLoadedService()

		id, err := svc.CreateNode(newFixtureUserID("system"), nil, "Welcome", newFixtureSlug("welcome"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		// file path: <tmp>/root/welcome.md (based on your existing tests + GeneratePath convention)
		p := filepath.Join(tmpDir, "root", "welcome.md")
		statTreePath(p)

		raw, err := os.ReadFile(p)
		Expect(err).To(Succeed(), "read file: %v",

			err,
		)

		frontmatter, _, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)

		Expect(PageIDFromString(strings.TrimSpace(frontmatter.LeafWikiID))).To(Equal(*id))
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiCreatedAt", Not(BeEmpty())),
			HaveField("LeafWikiUpdatedAt", Not(BeEmpty())),
		), "expected leafwiki timestamps to be set, got %#v", frontmatter)
		Expect(frontmatter).To(matchFrontmatterAuthors(newFixtureUserID("system"), newFixtureUserID("system")),
			"expected creator metadata to be set, got %#v", frontmatter)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node rejects case insensitive slug conflict", func() {
		svc, _ := newLoadedService()
		{

			_, err := svc.CreateNode(newFixtureUserID("system"), nil, "Alpha", newFixtureSlug("Alpha"), ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode alpha failed: %v",

				err)
		}
		{

			_, err := svc.CreateNode(newFixtureUserID("system"), nil, "Alpha Lower", newFixtureSlug("alpha"), ptrKind(NodeKindPage))
			Expect(err).To(MatchError(ErrPageAlreadyExists), "expected ErrPageAlreadyExists for case-insensitive conflict, got %v",

				err)
		}

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node allows same basename page and section twins", func() {
		svc, dataDir := newLoadedService()

		pageID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Sync Page", newFixtureSlug("sync"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode page failed: %v",

			err)

		sectionID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Sync Section", newFixtureSlug("sync"), ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode section twin failed: %v",

			err)

		statTreePath(filepath.Join(dataDir, "root", "sync.md"))
		statTreePath(filepath.Join(dataDir, "root", "sync", "index.md"))

		page, err := svc.FindPageByRoutePathAndKind(newFixtureRoutePath("sync"), NodeKindPage)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind page failed: %v",

			err,
		)

		Expect(page.ID).To(Equal(*pageID))

		section, err := svc.FindPageByRoutePathAndKind(newFixtureRoutePath("sync"), NodeKindSection)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind section failed: %v",

			err)

		Expect(section.ID).To(Equal(*sectionID))
		{

			_, err := svc.CreateNode(newFixtureUserID("system"), nil, "Duplicate Page", newFixtureSlug("SYNC"), ptrKind(NodeKindPage))
			Expect(err).To(MatchError(ErrPageAlreadyExists), "expected ErrPageAlreadyExists for same-kind duplicate, got %v",

				err)
		}

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("content path for node uses core read rules", func() {
		svc, dataDir := newLoadedService()

		sectionID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Docs", newFixtureSlug("docs"), ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode section failed: %v",

			err,
		)

		pageID, err := svc.CreateNode(newFixtureUserID("system"), nil, "Guide", newFixtureSlug("guide"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode page failed: %v",

			err)
		{

			err := os.Rename(filepath.Join(dataDir, "root", "docs", "index.md"), filepath.Join(dataDir, "root", "docs", "INDEX.MD"))
			Expect(err).To(Succeed(), "rename section index: %v",

				err)
		}
		{

			err := os.WriteFile(filepath.Join(dataDir, "root", "docs", "README.md"), []byte("# README\n"), 0o644)
			Expect(err).To(Succeed(), "write README: %v",

				err)
		}

		section, err := svc.GetPage(*sectionID)
		Expect(err).To(Succeed(), "GetPage section failed: %v",

			err)

		sectionPath, err := svc.ContentPathForNode(section.PageNode)
		Expect(err).To(Succeed(), "ContentPathForNode section failed: %v",

			err)
		Expect(sectionPath).
			To(Equal("docs/INDEX.MD"), "section content path = %q, want docs/INDEX.MD",

				sectionPath)

		page, err := svc.GetPage(*pageID)
		Expect(err).To(Succeed(), "GetPage page failed: %v",

			err)

		pagePath, err := svc.ContentPathForNode(page.PageNode)
		Expect(err).To(Succeed(), "ContentPathForNode page failed: %v",

			err)
		Expect(pagePath).To(
			Equal("guide.md"),
			"page content path = %q, want guide.md",

			pagePath,
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node rejects traversal slug", func() {
		svc, dataDir := newLoadedService()

		_, err := svc.CreateNode(newFixtureUserID("system"), nil, "Outside", newFixtureSlug("../outside"), ptrKind(NodeKindPage))
		Expect(err).To(MatchError(ErrInvalidOperation), "expected ErrInvalidOperation, got %v",

			err,
		)

		Expect(filepath.Join(dataDir, "outside.md")).To(beMissingTreePath())

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node persists root order file", func() {
		svc, tmpDir := newLoadedService()

		idA, err := svc.CreateNode(newFixtureUserID("system"), nil, "A", newFixtureSlug("a"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode A failed: %v",

			err)

		idB, err := svc.CreateNode(newFixtureUserID("system"), nil, "B", newFixtureSlug("b"), ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode B failed: %v",

			err)

		Expect(readOrderIDs(filepath.Join(tmpDir, "root"))).To(matchPersistedPageIDOrder(*idA, *idB))

	})
})

// - New section creates index.md
var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("create node section creates index with frontmatter", func() {
		svc, tmpDir := newLoadedService()

		id, err := svc.CreateNode(newFixtureUserID("system"), nil, "Docs", newFixtureSlug("docs"), ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode failed: %v",

			err)

		index := filepath.Join(tmpDir, "root", "docs", "index.md")
		raw, err := os.ReadFile(index)
		Expect(err).To(Succeed(), "read file: %v",

			err,
		)

		frontmatter, body, err := parseRequiredFrontmatter(string(raw))
		Expect(err).To(Succeed(), "ParseFrontmatter: %v", err)

		Expect(PageIDFromString(strings.TrimSpace(frontmatter.LeafWikiID))).To(Equal(*id))
		Expect(frontmatter.LeafWikiTitle).To(
			Equal(
				"Docs"), "expected leafwiki_title Docs, got %q",

			frontmatter.LeafWikiTitle)
		Expect(frontmatter).To(SatisfyAll(
			HaveField("LeafWikiCreatedAt", Not(BeEmpty())),
			HaveField("LeafWikiUpdatedAt", Not(BeEmpty())),
		), "expected leafwiki timestamps to be set, got %#v", frontmatter)
		Expect(frontmatter).To(matchFrontmatterAuthors(newFixtureUserID("system"), newFixtureUserID("system")),
			"expected creator metadata to be set, got %#v", frontmatter)
		Expect(strings.TrimSpace(body)).To(BeEmpty(),

			"expected empty section body, got %q",

			body)

	})
})

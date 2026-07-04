package tree

import (
	. "github.com/onsi/gomega"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("walk nodes visits nested nodes", func() {
		tmpDir := tempTreeDir()
		{
			err := saveSchema(tmpDir, CurrentSchemaVersion)
			Expect(err).To(Succeed(), "saveSchema failed: %v",

				err)
		}

		svc := NewTreeService(tmpDir)
		{
			err := svc.LoadTree()
			Expect(err).To(Succeed(), "LoadTree failed: %v",

				err)
		}

		parentID, err := svc.CreateNode("u", nil, "Parent", "parent", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode parent: %v",

			err)
		{

			_, err := svc.CreateNode("u", parentID, "Child", "child", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode child: %v",

				err)
		}

		var visited []string
		{
			err := svc.WalkNodes(func(id PageID) error {
				page, err := svc.GetPage(id)
				if err != nil {
					return err
				}
				visited = append(visited, page.Slug.String())
				return nil
			})
			Expect(err).To(Succeed(), "WalkNodes failed: %v",

				err)
		}
		Expect(visited).To(HaveLen(2),
			"expected 2 visited nodes (parent + child), got %d: %v",

			len(visited), visited)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("get pages preserves order and aligns errors", func() {
		svc, _ := newLoadedService()

		firstID, err := svc.CreateNode("system", nil, "First", "first", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode(first) failed: %v",

			err)

		secondID, err := svc.CreateNode("system", nil, "Second", "second", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode(second) failed: %v",

			err,
		)

		pages, errs := svc.GetPages([]PageID{*secondID, PageID("missing-id"), *firstID})
		Expect(pages).To(HaveLen(3), "unexpected page result length")
		Expect(errs).To(HaveLen(3), "unexpected error result length")
		pageResults := make([]pageLookupResult, 0, len(pages))
		for i := range pages {
			pageResults = append(pageResults, pageLookupResult{Page: pages[i], Err: errs[i]})
		}
		Expect(pageResults).To(HaveExactElements(
			SatisfyAll(
				HaveField("Page", pointToValue[Page](HaveField("ID", Equal(*secondID)))),
				HaveField("Err", Succeed()),
			),
			SatisfyAll(
				HaveField("Page", BeNil()),
				HaveField("Err", MatchError(ErrPageNotFound)),
			),
			SatisfyAll(
				HaveField("Page", pointToValue[Page](HaveField("ID", Equal(*firstID)))),
				HaveField("Err", Succeed()),
			),
		))

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("bulk update content treats frontmatter like input as body", func() {
		svc, _ := newLoadedService()

		firstID, err := svc.CreateNode("system", nil, "First", "first", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode(first) failed: %v",

			err)

		secondID, err := svc.CreateNode("system", nil, "Second", "second", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode(second) failed: %v",

			err,
		)

		beforeFirst, err := svc.GetPage(*firstID)
		Expect(err).To(Succeed(), "GetPage(first before) failed: %v",

			err)

		// Content that looks like invalid YAML frontmatter is now stored as plain
		// body text — UpsertContent no longer parses frontmatter from UI content.
		errs := svc.BulkUpdateContent("bulk-user", []BulkContentUpdate{
			{ID: *firstID, Content: "updated first"},
			{ID: *secondID, Content: "---\ninvalid: [\n---\nbody"},
			{ID: "missing-id", Content: "ignored"},
		})
		Expect(errs).To(HaveExactElements(
			Succeed(),
			Succeed(),
			MatchError(ErrPageNotFound),
		))

		// Index 1 now succeeds: frontmatter-like content is treated as plain body.

		afterFirst, err := svc.GetPage(*firstID)
		Expect(err).To(Succeed(), "GetPage(first after) failed: %v",

			err,
		)
		Expect(afterFirst).To(SatisfyAll(
			HaveField("Content", Equal("updated first")),
			HaveField("Metadata", SatisfyAll(
				HaveField("LastAuthorID", Equal(newFixtureUserID("bulk-user"))),
				HaveField("UpdatedAt", Or(
					BeTemporally(">", beforeFirst.Metadata.UpdatedAt),
					BeTemporally("==", beforeFirst.Metadata.UpdatedAt),
				)),
			)),
		), "expected first page content and metadata to reflect the bulk update")

		afterSecond, err := svc.GetPage(*secondID)
		Expect(err).To(Succeed(), "GetPage(second after) failed: %v",

			err)
		Expect(afterSecond.
			Content,
		).
			To(ContainSubstring("invalid: ["),

				"expected second content to contain plain body text, got %q",

				afterSecond.
					Content)

		// The "invalid YAML" block is now stored verbatim as body content.

	})
})

// ─────────────────────────────────────────────────────────────────────────────
// Optimistic locking: version check is enforced inside the write lock
// ─────────────────────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("update node stale version returns err version conflict", func() {
		svc, _ := newLoadedService()
		id, _ := svc.CreateNode("system", nil, "Page", "page", ptrKind(NodeKindPage))

		node, _ := svc.FindPageByID(*id)
		currentVersion := node.Version()
		{

			// First update succeeds — advances the version.
			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Page v2", Slug("page"), nil, PageVersionFromString(currentVersion), false)
			Expect(err).To(Succeed(), "first UpdateNode failed: %v",

				err)
		}

		// Second update with the same (now stale) version must fail.
		err := svc.UpdateNode(newFixtureUserID("system"), *id, "Page v3", Slug("page"), nil, PageVersionFromString(currentVersion), false)
		Expect(err).To(MatchError(ErrVersionConflict), "expected ErrVersionConflict, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("update node missing version returns err version required", func() {
		svc, _ := newLoadedService()
		id, _ := svc.CreateNode("system", nil, "Page", "page", ptrKind(NodeKindPage))

		err := svc.UpdateNode(newFixtureUserID("system"), *id, "Page v2", Slug("page"), nil, PageVersion(""), false)
		Expect(err).To(MatchError(ErrVersionRequired), "expected ErrVersionRequired, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("delete node stale version returns err version conflict", func() {
		svc, _ := newLoadedService()
		id, _ := svc.CreateNode("system", nil, "Page", "page", ptrKind(NodeKindPage))

		node, _ := svc.FindPageByID(*id)
		staleVersion := node.Version()
		{

			// Advance the version via an update.
			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Page v2", Slug("page"), nil, PageVersionFromString(staleVersion), false)
			Expect(err).To(Succeed(), "UpdateNode failed: %v",

				err)
		}

		err := svc.DeleteNode("system", *id, false, PageVersionFromString(staleVersion))
		Expect(err).To(MatchError(ErrVersionConflict), "expected ErrVersionConflict, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("delete node missing version returns err version required", func() {
		svc, _ := newLoadedService()
		id, _ := svc.CreateNode("system", nil, "Page", "page", ptrKind(NodeKindPage))

		err := svc.DeleteNode("system", *id, false, "")
		Expect(err).To(MatchError(ErrVersionRequired), "expected ErrVersionRequired, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node stale version returns err version conflict", func() {
		svc, _ := newLoadedService()
		destID, _ := svc.CreateNode("system", nil, "Dest", "dest", ptrKind(NodeKindPage))
		moveID, _ := svc.CreateNode("system", nil, "Move", "move", ptrKind(NodeKindPage))

		node, _ := svc.FindPageByID(*moveID)
		staleVersion := node.Version()
		{

			// Advance the version.
			err := svc.UpdateNode(newFixtureUserID("system"), *moveID, "Move v2", Slug("move"), nil, PageVersionFromString(staleVersion), false)
			Expect(err).To(Succeed(), "UpdateNode failed: %v",

				err)
		}

		err := svc.MoveNode("system", *moveID, *destID, PageVersionFromString(staleVersion))
		Expect(err).To(MatchError(ErrVersionConflict), "expected ErrVersionConflict, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("move node missing version returns err version required", func() {
		svc, _ := newLoadedService()
		destID, _ := svc.CreateNode("system", nil, "Dest", "dest", ptrKind(NodeKindPage))
		moveID, _ := svc.CreateNode("system", nil, "Move", "move", ptrKind(NodeKindPage))

		err := svc.MoveNode("system", *moveID, *destID, "")
		Expect(err).To(MatchError(ErrVersionRequired), "expected ErrVersionRequired, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("convert node stale version returns err version conflict", func() {
		svc, _ := newLoadedService()
		id, _ := svc.CreateNode("system", nil, "Page", "page", ptrKind(NodeKindPage))

		node, _ := svc.FindPageByID(*id)
		staleVersion := node.Version()
		{

			// Advance the version.
			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Page v2", Slug("page"), nil, PageVersionFromString(staleVersion), false)
			Expect(err).To(Succeed(), "UpdateNode failed: %v",

				err)
		}

		err := svc.ConvertNode("system", *id, NodeKindSection, PageVersionFromString(staleVersion))
		Expect(err).To(MatchError(ErrVersionConflict), "expected ErrVersionConflict, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("convert node missing version returns err version required", func() {
		svc, _ := newLoadedService()
		id, _ := svc.CreateNode("system", nil, "Page", "page", ptrKind(NodeKindPage))

		err := svc.ConvertNode("system", *id, NodeKindSection, "")
		Expect(err).To(MatchError(ErrVersionRequired), "expected ErrVersionRequired, got %v",

			err)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("version unchecked bypasses version check", func() {
		svc, _ := newLoadedService()
		id, _ := svc.CreateNode("system", nil, "Page", "page", ptrKind(NodeKindPage))
		{

			// The tree-owned unchecked operation must always succeed regardless of actual node version.
			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Page v2", Slug("page"), nil, pageVersionUnchecked, false)
			Expect(err).To(Succeed(), "expected unchecked operation to bypass check, got: %v",

				err,
			)
		}
		{

			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Page v3", Slug("page"), nil, pageVersionUnchecked, false)
			Expect(err).To(Succeed(), "expected unchecked operation to bypass check on second call, got: %v",

				err)
		}

	})
})

// ─── RawContent ───────────────────────────────────────────────────────────────

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("get page raw content contains canonical metadata and body", func() {
		svc, _ := newLoadedService()

		id, err := svc.CreateNode("system", nil, "Raw Test", "raw-test", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode: %v",

			err,
		)

		body := "Hello raw world"
		page, err := svc.GetPage(*id)
		Expect(err).To(Succeed(), "GetPage before update: %v",

			err)
		{

			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Raw Test", Slug("raw-test"), &body, PageVersionFromString(page.Version()), false)
			Expect(err).To(Succeed(), "UpdateNode: %v",

				err,
			)
		}

		page, err = svc.GetPage(*id)
		Expect(err).To(Succeed(), "GetPage: %v",

			err)
		Expect(page).To(SatisfyAll(
			HaveField("RawContent", SatisfyAll(
				Not(BeEmpty()),
				ContainSubstring("<!-- leafwiki\n"),
				ContainSubstring("Hello raw world"),
			)),
			HaveField("Content", SatisfyAll(
				WithTransform(strings.TrimSpace, Not(HavePrefix("<!-- leafwiki"))),
				Not(Equal(page.RawContent)),
			)),
		), "expected page to expose body content separately from raw storage, got %#v", page)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("get pages raw content populated for all", func() {
		svc, _ := newLoadedService()

		id1, err := svc.CreateNode("system", nil, "Page One", "page-one", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode 1: %v",

			err)

		id2, err := svc.CreateNode("system", nil, "Page Two", "page-two", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode 2: %v",

			err)

		pages, errs := svc.GetPages([]PageID{*id1, *id2})
		Expect(errs).To(HaveEach(Succeed()))
		for i, p := range pages {
			Expect(p.RawContent).
				NotTo(BeEmpty(),

					"GetPages[%d]: expected RawContent to be populated",

					i)
			Expect(p.RawContent).To(ContainSubstring("<!-- leafwiki\n"),

				"GetPages[%d]: expected RawContent to contain canonical metadata, got: %q",

				i, p.RawContent)

		}

	})
})

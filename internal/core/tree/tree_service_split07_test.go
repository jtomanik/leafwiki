package tree

import (
	. "github.com/onsi/gomega"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("lookup page path segments", func() {
		svc, _ := newLoadedService()

		homeID, _ := svc.CreateNode("system", nil, "Home", "home", ptrKind(NodeKindPage))
		aboutID, _ := svc.CreateNode("system", homeID, "About", "about", ptrKind(NodeKindPage))

		lookup, err := svc.LookupPagePath("home/about/team")
		Expect(err).To(Succeed(), "LookupPagePath failed: %v",

			err)
		Expect(lookup).To(matchMissingPathLookup(HaveExactElements(
			matchExistingPathSegment(*homeID),
			matchExistingPathSegment(*aboutID),
			matchMissingPathSegment(),
		)),
			"expected lookup to resolve existing ancestors and report the missing leaf, got %#v", lookup)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("lookup page path is case insensitive", func() {
		svc, _ := newLoadedService()

		homeID, err := svc.CreateNode("system", nil, "Home", "Home", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode home failed: %v",

			err)
		var aboutID *PageID
		{

			aboutID, err = svc.CreateNode("system", homeID, "About", "About", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode about failed: %v",

				err)
		}

		lookup, err := svc.LookupPagePath("home/about")
		Expect(err).To(Succeed(), "LookupPagePath failed: %v",

			err)
		Expect(lookup).To(matchExistingPathLookup(HaveExactElements(
			matchExistingPathSegment(*homeID),
			matchExistingPathSegment(*aboutID),
		)), "expected case-insensitive path lookup to resolve existing path")

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("lookup page path prefers section for same basename twin", func() {
		svc, _ := newLoadedService()

		sectionID, err := svc.CreateNode("system", nil, "Sync Section", "sync", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode section failed: %v",

			err,
		)
		{

			_, err := svc.CreateNode("system", nil, "Sync Page", "sync", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode page twin failed: %v",

				err,
			)
		}

		lookup, err := svc.LookupPagePath("sync")
		Expect(err).To(Succeed(), "LookupPagePath failed: %v",

			err)
		Expect(lookup).To(matchExistingPathLookup(HaveExactElements(SatisfyAll(
			matchExistingPathSegment(*sectionID),
			HaveField("Kind", pointToValue[NodeKind](Equal(NodeKindSection))),
		))),
			"lookup = %#v, want existing section segment", lookup)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("lookup page path reflects slug rename", func() {
		svc, _ := newLoadedService()

		id, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode docs failed: %v",

			err)
		var guideID *PageID
		{

			guideID, err = svc.CreateNode("system", id, "Guide", "guide", ptrKind(NodeKindPage))
			Expect(err).To(Succeed(), "CreateNode guide failed: %v",

				err)
		}
		{

			err := svc.UpdateNode(newFixtureUserID("system"), *id, "Documentation", Slug("documentation"), nil, pageVersionUnchecked, false)
			Expect(err).To(Succeed(), "UpdateNode failed: %v",

				err)
		}

		oldLookup, err := svc.LookupPagePath("docs/guide")
		Expect(err).To(Succeed(), "LookupPagePath old path failed: %v",

			err)
		Expect(oldLookup).To(matchMissingPathLookup(HaveExactElements(
			matchMissingPathSegment(),
			matchMissingPathSegment(),
		)), "expected old path to stop resolving after slug rename")

		newLookup, err := svc.LookupPagePath("documentation/guide")
		Expect(err).To(Succeed(), "LookupPagePath new path failed: %v",

			err)
		Expect(newLookup).To(matchExistingPathLookup(HaveExactElements(
			matchExistingPathSegment(*id),
			matchExistingPathSegment(*guideID),
		)), "expected renamed path to resolve")

		page, err := svc.FindPageByRoutePath("documentation/guide")
		Expect(err).To(Succeed(), "FindPageByRoutePath renamed path failed: %v",

			err)
		Expect(page.Slug).To(Equal(newFixtureSlug("guide")),
			"expected guide page, got %q",

			page.
				Slug)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("lookup page path can create for missing valid path", func() {
		svc, _ := newLoadedService()

		lookup, err := svc.LookupPagePath("docs/guide")
		Expect(err).To(Succeed(), "LookupPagePath failed: %v",

			err)
		Expect(lookup.CanCreate).To(BeTrue(),
			"expected missing valid path to be creatable",
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("lookup page path cannot create reserved missing path", func() {
		svc, _ := newLoadedService()

		lookup, err := svc.LookupPagePath("history/guide")
		Expect(err).To(Succeed(), "LookupPagePath failed: %v",

			err)
		Expect(lookup.CanCreate).To(BeFalse(),
			"expected reserved slug path to be non-creatable",
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("resolve permalink target reflects rename and move", func() {
		svc, _ := newLoadedService()

		docsID, err := svc.CreateNode("system", nil, "Docs", "docs", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode docs failed: %v",

			err)

		guideID, err := svc.CreateNode("system", docsID, "Guide", "guide", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode guide failed: %v",

			err)

		archiveID, err := svc.CreateNode("system", nil, "Archive", "archive", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode archive failed: %v",

			err,
		)
		{

			err := svc.UpdateNode(newFixtureUserID("system"), *guideID, "User Guide", Slug("user-guide"), nil, pageVersionUnchecked, false)
			Expect(err).To(Succeed(), "UpdateNode guide failed: %v",

				err)
		}
		{

			err := svc.MoveNode("system", *guideID, *archiveID, pageVersionUnchecked)
			Expect(err).To(Succeed(), "MoveNode guide failed: %v",

				err)
		}

		target, err := svc.ResolvePermalinkTarget(*guideID)
		Expect(err).To(Succeed(), "ResolvePermalinkTarget failed: %v",

			err)

		Expect(target).To(SatisfyAll(
			HaveField("ID", Equal(*guideID)),
			HaveField("Slug", Equal(newFixtureSlug("user-guide"))),
			HaveField("Path", Equal("archive/user-guide")),
		), "expected permalink to resolve the archived guide, got %#v", target)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("resolve permalink target returns not found for missing page", func() {
		svc, _ := newLoadedService()

		_, err := svc.ResolvePermalinkTarget("missing-page")
		Expect(err).To(MatchError(ErrPageNotFound),
			"expected ErrPageNotFound, got %v",

			err,
		)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("ensure page path persists order files for created path", func() {
		svc, tmpDir := newLoadedService()

		res, err := svc.EnsurePagePath("system", "home/about/team/members", "Members", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "EnsurePagePath failed: %v",

			err)
		Expect(res.Page).To(SatisfyAll(
			Not(BeNil()),
			HaveField("Slug", Equal(newFixtureSlug("members"))),
		), "expected final page 'members'")

		rootOrder := readOrderIDs(filepath.Join(tmpDir, "root"))
		Expect(rootOrder).To(matchPersistedPageIDOrder(res.Created[0].ID),
			"unexpected root order after EnsurePagePath: %v", rootOrder)

		homeOrder := readOrderIDs(filepath.Join(tmpDir, "root", "home"))
		Expect(homeOrder).To(matchPersistedPageIDOrder(res.Created[1].ID),
			"unexpected home order after EnsurePagePath: %v", homeOrder)

		aboutOrder := readOrderIDs(filepath.Join(tmpDir, "root", "home", "about"))
		Expect(aboutOrder).To(matchPersistedPageIDOrder(res.Created[2].ID),
			"unexpected about order after EnsurePagePath: %v", aboutOrder)

		teamOrder := readOrderIDs(filepath.Join(tmpDir, "root", "home", "about", "team"))
		Expect(teamOrder).To(matchPersistedPageIDOrder(res.Created[3].ID),
			"unexpected team order after EnsurePagePath: %v", teamOrder)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("ensure page path creates intermediate sections and final page", func() {
		svc, _ := newLoadedService()

		// Ensure a deep path; intermediate nodes should become sections
		res, err := svc.EnsurePagePath("system", "home/about/team/members", "Members", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "EnsurePagePath failed: %v",

			err)
		Expect(res.Page).To(SatisfyAll(
			Not(BeNil()),
			HaveField("Slug", Equal(newFixtureSlug("members"))),
		), "expected final page 'members'")

		// home/about/team should exist as path now
		lookup, err := svc.LookupPagePath("home/about/team/members")
		Expect(err).To(Succeed(), "LookupPagePath failed: %v",

			err)
		Expect(lookup).To(matchExistingPathLookup(HaveExactElements(
			matchExistingPathSegment(res.Created[0].ID),
			matchExistingPathSegment(res.Created[1].ID),
			matchExistingPathSegment(res.Created[2].ID),
			matchExistingPathSegment(res.Created[3].ID),
		)), "expected path to exist after EnsurePagePath")

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("ensure page path returns existing page without creating nodes", func() {
		svc, _ := newLoadedService()

		res, err := svc.EnsurePagePath("system", "home/about/team/members", "Members", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "EnsurePagePath initial create failed: %v",

			err,
		)

		existing, err := svc.EnsurePagePath("system", "home/about/team/members", "Ignored", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "EnsurePagePath existing failed: %v",

			err)
		Expect(existing).To(matchExistingEnsurePathResult(matchTreeNodePointer(NodeKindPage, Equal(res.Page.ID))),
			"expected EnsurePagePath to return the existing page without creating nodes, got %#v", existing)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("ensure page path creates page twin when section route exists", func() {
		svc, _ := newLoadedService()

		sectionID, err := svc.CreateNode("system", nil, "Sync Section", "sync", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "CreateNode section failed: %v",

			err,
		)

		res, err := svc.EnsurePagePath("system", "sync", "Sync Page", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "EnsurePagePath page twin failed: %v",

			err)
		Expect(res).To(SatisfyAll(
			HaveField("Page", matchTreeNodePointer(NodeKindPage, Not(Equal(*sectionID)))),
			HaveField("Created", HaveExactElements(
				matchTreeNodePointer(NodeKindPage, Not(Equal(*sectionID))),
			)),
		), "expected EnsurePagePath to create one page twin, got %#v", res)

		section, err := svc.FindPageByRoutePathAndKind("sync", NodeKindSection)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind section failed: %v",

			err)

		Expect(section.ID).To(Equal(*sectionID))
		page, err := svc.FindPageByRoutePathAndKind("sync", NodeKindPage)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind page failed: %v",

			err,
		)
		Expect(page.ID).To(Equal(res.Page.
			ID),
			"page route ID = %q, want %q",

			page.
				ID, res.
				Page.ID,
		)

		second, err := svc.EnsurePagePath("system", "sync", "Ignored", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "EnsurePagePath existing page twin failed: %v",

			err)
		Expect(second).To(matchExistingEnsurePathResult(matchTreeNodePointer(NodeKindPage, Equal(res.Page.ID))),
			"expected second ensure to return the existing page twin, got %#v", second)

	})
})

var _ = ginkgo.Describe("tree service behavior", ginkgo.Label("unit"), func() {
	ginkgo.It("ensure page path creates section twin when page route exists", func() {
		svc, _ := newLoadedService()

		pageID, err := svc.CreateNode("system", nil, "Sync Page", "sync", ptrKind(NodeKindPage))
		Expect(err).To(Succeed(), "CreateNode page failed: %v",

			err)

		res, err := svc.EnsurePagePath("system", "sync", "Sync Section", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "EnsurePagePath section twin failed: %v",

			err)
		Expect(res).To(SatisfyAll(
			HaveField("Page", matchTreeNodePointer(NodeKindSection, Not(Equal(*pageID)))),
			HaveField("Created", HaveExactElements(
				matchTreeNodePointer(NodeKindSection, Not(Equal(*pageID))),
			)),
		), "expected EnsurePagePath to create one section twin, got %#v", res)

		page, err := svc.FindPageByRoutePathAndKind("sync", NodeKindPage)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind page failed: %v",

			err,
		)

		Expect(page.ID).To(Equal(*pageID))
		section, err := svc.FindPageByRoutePathAndKind("sync", NodeKindSection)
		Expect(err).To(Succeed(), "FindPageByRoutePathAndKind section failed: %v",

			err)
		Expect(section.ID).To(Equal(res.
			Page.
			ID), "section route ID = %q, want %q",

			section.
				ID, res.
				Page.ID)

		second, err := svc.EnsurePagePath("system", "sync", "Ignored", ptrKind(NodeKindSection))
		Expect(err).To(Succeed(), "EnsurePagePath existing section twin failed: %v",

			err)
		Expect(second).To(matchExistingEnsurePathResult(matchTreeNodePointer(NodeKindSection, Equal(res.Page.ID))),
			"expected second ensure to return the existing section twin, got %#v", second)

	})
})

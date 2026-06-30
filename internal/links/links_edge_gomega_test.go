package links

import (
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

func newManualLinksStore(storageDir string) *LinksStore {
	GinkgoHelper()

	store := &LinksStore{
		storageDir:   storageDir,
		databaseFile: "links.db",
	}
	Expect(store.Connect()).To(Succeed())
	DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
	return store
}

func HaveOutgoingKindsForPage(pageID tree.PageID, kinds ...TargetKind) types.GomegaMatcher {
	GinkgoHelper()
	return WithTransform(func(store *LinksStore) ([]TargetKind, error) {
		outgoing, err := store.GetOutgoingLinksForPage(pageID)
		if err != nil {
			return nil, err
		}
		got := make([]TargetKind, 0, len(outgoing))
		for _, link := range outgoing {
			got = append(got, link.ToKind)
		}
		return got, nil
	}, ConsistOf(kinds))
}

func matchBacklink(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchOutgoingResultItem(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchRewriteResult(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

var _ = Describe("links edge coverage", func() {
	It("migrates a legacy links table without target kinds", func() {
		store := newManualLinksStore(GinkgoT().TempDir())
		_, err := store.db.Exec(`
			CREATE TABLE links (
				from_page_id TEXT NOT NULL,
				to_page_id   TEXT,
				to_path      TEXT NOT NULL,
				from_title   TEXT,
				broken       INTEGER NOT NULL DEFAULT 0,
				PRIMARY KEY (from_page_id, to_path)
			);
			INSERT INTO links(from_page_id, to_page_id, to_path, from_title, broken)
			VALUES ('legacy-source', 'target-page', '/docs/topic', 'Legacy Source', 0);
		`)
		Expect(err).NotTo(HaveOccurred())

		Expect(store.ensureLinksTable()).To(Succeed())

		columns, err := store.linksTableColumns()
		Expect(err).NotTo(HaveOccurred())
		Expect(linksTableKindAware(columns)).To(BeTrue())
		Expect(linksTableHasColumn(columns, "to_kind")).To(BeTrue())
		Expect(store).To(HaveOutgoingKindsForPage(newFixturePageID("legacy-source"), defaultStoredTargetKind))
	})

	It("migrates a legacy links table with non-key target kind values", func() {
		store := newManualLinksStore(GinkgoT().TempDir())
		_, err := store.db.Exec(`
			CREATE TABLE links (
				from_page_id TEXT NOT NULL,
				to_page_id   TEXT,
				to_path      TEXT NOT NULL,
				to_kind      TEXT,
				from_title   TEXT,
				broken       INTEGER NOT NULL DEFAULT 0,
				PRIMARY KEY (from_page_id, to_path)
			);
			INSERT INTO links(from_page_id, to_page_id, to_path, to_kind, from_title, broken)
			VALUES
				('blank-kind-source', 'target-page', '/docs/page', '', 'Blank Kind Source', 0),
				('section-kind-source', 'target-section', '/docs/section', 'section', 'Section Kind Source', 0);
		`)
		Expect(err).NotTo(HaveOccurred())

		Expect(store.ensureLinksTable()).To(Succeed())

		Expect(store).To(HaveOutgoingKindsForPage(newFixturePageID("blank-kind-source"), defaultStoredTargetKind))
		Expect(store).To(HaveOutgoingKindsForPage(newFixturePageID("section-kind-source"), sectionStoredTargetKind))
	})

	It("exercises unfiltered prefix queries and non-kind broken-link wrappers", func() {
		store := newAdditionalLinksStore()
		service := NewLinkService("", nil, store)
		Expect(seedAdditionalLinks(store)).To(Succeed())

		matches, err := service.GetRefactorMatchesForPrefix("/docs/topic")
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).To(HaveLen(3))

		sourceIDs, err := service.GetRefactorSourcePageIDsForPrefix("/docs/topic")
		Expect(err).NotTo(HaveOccurred())
		Expect(sourceIDs).To(ConsistOf(
			newFixturePageID("source-page"),
			newFixturePageID("section-source"),
			newFixturePageID("child-source"),
		))

		Expect(service.MarkIncomingLinksBrokenForPage(newFixturePageID("target-page"))).To(Succeed())
		brokenPage, err := store.GetBrokenIncomingForPathAndKind("/docs/topic", tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(brokenPage).To(ConsistOf(matchBacklink(gstruct.Fields{
			"FromPageID": Equal(newFixturePageID("source-page")),
			"Broken":     BeTrue(),
		})))

		Expect(store.HealLinksForPath("/docs/topic", newFixturePageID("healed-page"))).To(Succeed())
		healed, err := store.GetBacklinksForPage(newFixturePageID("healed-page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(healed).NotTo(BeEmpty())

		Expect(service.MarkLinksBrokenForPath("docs/topic")).To(Succeed())
		allBroken, err := store.GetBrokenIncomingForPath("/docs/topic")
		Expect(err).NotTo(HaveOccurred())
		Expect(allBroken).To(HaveLen(2))

		Expect(service.MarkLinksBrokenForPrefix("docs/topic")).To(Succeed())
		descendant, err := store.GetBrokenIncomingForPath("/docs/topic/child")
		Expect(err).NotTo(HaveOccurred())
		Expect(descendant).To(HaveLen(1))
	})

	It("closes stores and exposes the underlying database handle defensively", func() {
		store := newAdditionalLinksStore()
		service := NewLinkService("", nil, store)

		Expect(store.GetDB()).NotTo(BeNil())
		Expect(service.Close()).To(Succeed())
		Expect(store.GetDB()).To(BeNil())
		Expect(service.Close()).To(Succeed())
		Expect(NewLinkService("", nil, nil).Close()).To(Succeed())
	})

	It("covers pure path, suffix, and target-kind helpers", func() {
		Expect(normalizeWikiPath("docs//topic/?x=1#frag")).To(Equal("/docs/topic"))
		Expect(normalizeWikiPath("")).To(BeEmpty())
		Expect(normalizeWikiPath("/")).To(Equal("/"))

		base, suffix := splitLinkDestinationSuffix("docs/page.md#heading")
		Expect(base).To(Equal("docs/page.md"))
		Expect(suffix).To(Equal("#heading"))
		base, suffix = splitLinkDestinationSuffix("docs/page.md?x=1")
		Expect(base).To(Equal("docs/page.md"))
		Expect(suffix).To(Equal("?x=1"))
		base, suffix = splitLinkDestination("")
		Expect(base).To(BeEmpty())
		Expect(suffix).To(BeEmpty())

		Expect(isExtensionlessWikiDestination("docs/page")).To(BeTrue())
		Expect(isExtensionlessWikiDestination("docs/page/")).To(BeFalse())
		Expect(isExtensionlessWikiDestination("docs/page.md?x=1")).To(BeFalse())

		Expect(unresolvedStoredTargetKind(markdownlinksResolution(markdownlinks.IssueCodeBrokenPage), "docs/page")).To(Equal(TargetKindPage))
		Expect(unresolvedStoredTargetKind(markdownlinksResolution(markdownlinks.IssueCodeBrokenLink), "docs/section/")).To(Equal(TargetKindSection))
		Expect(unresolvedStoredTargetKind(markdownlinksResolution(markdownlinks.IssueCodeBrokenLink), "docs/page.md")).To(Equal(TargetKindPage))
		Expect(unresolvedStoredTargetKind(markdownlinksResolution(markdownlinks.IssueCodeBrokenLink), "docs/unknown")).To(Equal(unknownStoredTargetKind))

		Expect(storedTargetMarkdownHref("/", defaultStoredTargetKind)).To(Equal("/"))
		Expect(storedTargetMarkdownHref("/docs/page.md", defaultStoredTargetKind)).To(Equal("/docs/page.md"))
		Expect(storedTargetMarkdownHref("/docs/section", sectionStoredTargetKind)).To(Equal("/docs/section"))

		Expect(relativeWikiLinkPath("/docs/source", "/docs/target")).To(Equal("../target"))
		Expect(relativeWikiLinkPath("/docs/source", "/other/target")).To(Equal("../../other/target"))
		Expect(relativeWikiLinkPath("/same", "/same")).To(BeEmpty())
		Expect(splitWikiPathSegments("/docs/topic")).To(Equal([]string{"docs", "topic"}))
		Expect(splitWikiPathSegments("/")).To(BeNil())

		Expect(relativeMarkdownFileLinkPath("/docs/source", "/docs/target")).To(Equal("target.md"))
		Expect(relativeMarkdownDestinationForSource("/docs/source", MarkdownSourceKindSection, "/docs/source", false)).To(Equal("."))
		Expect(sourceMarkdownFileForKind("/docs/source", MarkdownSourceKindSection).FilesystemPath()).To(Equal(filepath.ToSlash("docs/source/index.md")))
	})

	It("deduplicates rewrite warnings and exercises rewrite-rule wrappers", func() {
		warnings := dedupeWarnings([]RewriteWarning{
			{Message: "same"},
			{Message: "same"},
			{Message: "different"},
		})
		Expect(warnings).To(Equal([]RewriteWarning{
			{Message: "same"},
			{Message: "different"},
		}))

		rewritten, ok := applyRewriteRules("/docs/old", []RewriteRule{{
			OldPath: "/docs/old",
			NewPath: "/docs/new",
		}})
		Expect(ok).To(BeTrue())
		Expect(rewritten).To(Equal(tree.RoutePathFromString("/docs/new").Clean()))

		_, ok = applyRewriteRulesForKind("/docs/old/child", TargetKindPage, []RewriteRule{{
			OldPath: "/docs/old",
			NewPath: "/docs/new",
			Kind:    TargetKindPage,
		}})
		Expect(ok).To(BeFalse())

		_, ok = applyRewriteRulesForKind("/docs/old/child", TargetKindPage, []RewriteRule{{
			OldPath:    "/docs/old",
			NewPath:    "/docs/new",
			OutputKind: TargetKindPage,
		}})
		Expect(ok).To(BeFalse())

		Expect(rewriteRuleMatchesExactKind(RewriteRule{Kind: TargetKindPage}, "")).To(BeFalse())
	})

	It("uses the in-memory markdown index fallback for loaded trees", func() {
		root := &tree.PageNode{
			Kind: tree.NodeKindSection,
			Children: []*tree.PageNode{
				{
					ID:    newFixturePageID("docs-section"),
					Title: "Docs",
					Slug:  newFixtureSlug("docs"),
					Kind:  tree.NodeKindSection,
					Children: []*tree.PageNode{{
						ID:    newFixturePageID("topic-page"),
						Title: "Topic",
						Slug:  newFixtureSlug("topic"),
						Kind:  tree.NodeKindPage,
					}},
				},
			},
		}
		root.Children[0].Parent = root
		root.Children[0].Children[0].Parent = root.Children[0]

		index := markdownLinkIndexFromLoadedTree(root)
		resolved := index.Resolve("docs/index.md", "topic.md")
		Expect(resolved.RoutePath).To(Equal(tree.RoutePathFromString("/docs/topic").Clean()))

		indexWithPrefix := markdownLinkIndexFromLoadedTreeWithOptions(root, markdownlinks.Options{MarkdownLinkRootPrefix: "/wiki"})
		resolved = indexWithPrefix.Resolve("docs/index.md", "/wiki/docs/topic.md")
		Expect(resolved.RoutePath).To(Equal(tree.RoutePathFromString("/docs/topic").Clean()))
	})

	It("covers unloaded tree guards and fallback result mapping", func() {
		unloadedTree := tree.NewTreeService(GinkgoT().TempDir())

		Expect(resolveTargetLinksForSourceKind(unloadedTree, "/docs/source", tree.NodeKindPage, []string{"/docs/target.md"})).To(BeNil())
		Expect(resolveTargetLinksWithIndex(nil, nil, "/docs/source", tree.NodeKindPage, []string{"/docs/target.md"})).To(BeNil())

		loadedTree := newLoadedLinksTreeService()
		_ = resolveTargetLinksWithIndex(loadedTree, nil, "/docs/source", tree.NodeKindPage, []string{"missing.md"})

		Expect(markdownSourceFileForRoute("", tree.NodeKindSection)).To(Equal(tree.MarkdownPathFromString("index.md")))
		Expect(markdownSourceFileForRoute("/docs/ bad", tree.NodeKindPage)).To(BeEmpty())

		Expect(toBacklinkResultItem(unloadedTree, Backlink{FromPageID: newFixturePageID("source-page")})).To(Equal(BacklinkResultItem{}))
		Expect(toBacklinkResultItem(loadedTree, Backlink{FromPageID: newFixturePageID("missing-page")})).To(Equal(BacklinkResultItem{}))

		outgoing := toOutgoingResultItem(unloadedTree, Outgoing{
			FromPageID: newFixturePageID("source-page"),
			ToPageID:   newFixturePageID("target-page"),
			ToPath:     "/docs/target",
			ToKind:     nonCanonicalPageStoredTarget,
		})
		Expect(outgoing).To(matchOutgoingResultItem(gstruct.Fields{
			"ToKind":      Equal(defaultStoredTargetKind),
			"ToPageTitle": BeEmpty(),
		}))

		outgoing = toOutgoingResultItem(loadedTree, Outgoing{
			FromPageID: newFixturePageID("source-page"),
			ToPageID:   newFixturePageID("missing-page"),
			ToPath:     "/docs/target",
			ToKind:     defaultStoredTargetKind,
		})
		Expect(outgoing).To(matchOutgoingResultItem(gstruct.Fields{
			"ToPageTitle": BeEmpty(),
		}))
	})

	It("returns service errors from a closed database without relying on nil-store panics", func() {
		loadedTree := newLoadedLinksTreeService()
		page := createLoadedLinksPage(loadedTree, "Source", "source", "[Target](target.md)")
		store := newSQLClosedLinksStore()
		service := NewLinkService("", loadedTree, store)

		Expect(service.ClearLinks()).To(HaveOccurred())
		Expect(service.IndexAllPages()).To(HaveOccurred())

		_, err := service.GetLinkStatusForPage(page.ID, page.CalculateRoutePath())
		Expect(err).To(HaveOccurred())
		Expect(service.UpdateLinksForPage(page, page.Content)).To(HaveOccurred())
		Expect(service.UpdateRewrittenLinksAndHealForPages([]*tree.Page{page}, nil)).To(HaveOccurred())
	})

	It("returns store errors from a closed database handle", func() {
		store := newSQLClosedLinksStore()
		pageID := newFixturePageID("source-page")
		target := tree.RoutePathFromString("/docs/target")

		Expect(store.ensureSchema()).To(HaveOccurred())
		Expect(store.ensureLinksTable()).To(HaveOccurred())
		_, err := store.linksTableColumns()
		Expect(err).To(HaveOccurred())
		Expect(store.migrateLinksTableToKindAware(nil)).To(HaveOccurred())

		Expect(store.DeleteOutgoingLinks(pageID)).To(HaveOccurred())
		Expect(store.MarkIncomingLinksBroken(newFixturePageID("target-page"))).To(HaveOccurred())
		Expect(store.MarkLinksBrokenForPath(target)).To(HaveOccurred())
		Expect(store.MarkLinksBrokenForPathAndKind(target, tree.NodeKindPage)).To(HaveOccurred())
		Expect(store.MarkLinksBrokenForPrefix("/docs")).To(HaveOccurred())
		Expect(store.MarkLinksBrokenForPrefixAndKind("/docs", tree.NodeKindPage)).To(HaveOccurred())
		Expect(store.MarkLinksBrokenForPrefixAndKind("/docs", tree.NodeKindSection)).To(HaveOccurred())
		Expect(store.AddLinks(pageID, "Source", []TargetLink{{
			TargetPageID:   newFixturePageID("target-page"),
			TargetPagePath: target.WikiPath(),
			TargetKind:     TargetKindPage,
		}})).To(HaveOccurred())
		Expect(store.ReplaceLinksAndHeal([]PageLinkUpdate{{
			FromPageID: pageID,
			FromTitle:  "Source",
			ToPath:     "/docs/source",
			ToKind:     tree.NodeKindPage,
		}})).To(HaveOccurred())

		_, err = store.GetBacklinksForPage(newFixturePageID("target-page"))
		Expect(err).To(HaveOccurred())
		_, err = store.GetOutgoingLinksForPage(pageID)
		Expect(err).To(HaveOccurred())
		_, err = store.GetOutgoingLinksForPages([]tree.PageID{pageID})
		Expect(err).To(HaveOccurred())
		_, err = store.GetRefactorMatchesForPrefix("/docs")
		Expect(err).To(HaveOccurred())
		_, err = store.GetRefactorMatchesForPrefixAndKind("/docs", tree.NodeKindPage)
		Expect(err).To(HaveOccurred())
		_, err = store.GetRefactorMatchesForPrefixAndKind("/docs", tree.NodeKindSection)
		Expect(err).To(HaveOccurred())
		_, err = store.GetRefactorSourcePageIDsForPrefix("/docs")
		Expect(err).To(HaveOccurred())
		_, err = store.GetRefactorSourcePageIDsForPrefixAndKind("/docs", tree.NodeKindPage)
		Expect(err).To(HaveOccurred())
		_, err = store.GetRefactorSourcePageIDsForPrefixAndKind("/docs", tree.NodeKindSection)
		Expect(err).To(HaveOccurred())
		_, err = store.GetBrokenIncomingForPath(target)
		Expect(err).To(HaveOccurred())
		_, err = store.GetBrokenIncomingForPathAndKind(target, tree.NodeKindPage)
		Expect(err).To(HaveOccurred())
	})

	It("covers no-op returns and upward path-change normalization in markdown refactoring", func() {
		engine := NewMarkdownRefactorEngine()
		rules := []RewriteRule{{
			OldPath: "/docs/old",
			NewPath: "/docs/new",
		}}

		Expect(engine.Rewrite("", "/docs/source", rules).Content).To(BeEmpty())
		Expect(engine.Rewrite("[No links text]", "/docs/source", rules).Content).To(Equal("[No links text]"))
		Expect(engine.RewriteRelativeLinksForPathChange("", "/docs/old", "/docs/new", rules).Content).To(BeEmpty())
		Expect(engine.RewriteRelativeLinksForPathChange("[No links text]", "/docs/old", "/docs/new", rules).Content).To(Equal("[No links text]"))

		result := engine.RewriteRelativeLinksForPathChange("[Bad](../../escape.md)", "/docs/source", "/docs/moved", nil)
		Expect(result).To(SatisfyAll(
			matchRewriteResult(gstruct.Fields{
				"Content":  Equal("[Bad](../index.md)"),
				"Warnings": BeEmpty(),
			}),
			HaveField("Count()", Equal(1)),
		))
	})

	It("covers defensive markdown resolution and index fallback branches", func() {
		loadedTree := newLoadedLinksTreeService()
		index := markdownlinks.NewIndex([]markdownlinks.Entry{{
			Kind:      markdownlinks.EntryKindPage,
			RoutePath: tree.RoutePathFromString("/ghost").Clean(),
		}})

		targets := resolveTargetLinksWithIndex(loadedTree, index, "/docs/source", tree.NodeKindPage, []string{
			"%zz",
			"/",
			"/ghost.md",
		})
		Expect(targets).To(Equal([]TargetLink{{
			TargetPagePath: "/ghost",
			TargetKind:     TargetKindPage,
			Broken:         true,
		}}))

		Expect(isExtensionlessWikiDestination("docs/page#heading")).To(BeTrue())
		Expect(markdownLinkIndexForTreeWithOptions(nil, markdownlinks.Options{})).NotTo(BeNil())

		restoreIndex := setLinksSeam(&newMarkdownLinkIndexFromRoot, func(string) (*markdownlinks.Index, error) {
			return nil, errors.New("index from root failed")
		})
		Expect(markdownLinkIndexForTree(loadedTree)).NotTo(BeNil())
		restoreIndex()

		Expect(markdownLinkIndexFromLoadedTreeWithOptions(nil, markdownlinks.Options{})).NotTo(BeNil())
	})

	It("covers remaining rewrite warning and path fallback branches", func() {
		rules := []RewriteRule{{
			OldPath: "/docs/old",
			NewPath: "/docs/new",
		}}
		restoreResolve := setLinksSeam(&linksResolveMarkdownRoutePath, func(tree.MarkdownPath, string) (tree.RoutePath, error) {
			return "", errors.New("resolve failed")
		})

		_, warnings := buildRewritePlan(
			"/docs/source",
			MarkdownSourceKindPage,
			rules,
			[]rewriteCandidate{{Destination: "../../../escape.md"}},
			[]markdownlinks.InlineDestination{{Destination: "../../../escape.md"}},
			"",
		)
		Expect(warnings).To(ConsistOf(testmatchers.HaveMessageID(rewriteWarningUnresolved)))

		_, changed, warning := rewriteLinkDestination("/docs/source", MarkdownSourceKindPage, "../../../escape.md", rules, "")
		Expect(changed).To(BeFalse())
		Expect(warning).NotTo(BeNil())
		Expect(warning).To(testmatchers.HaveMessageID(rewriteWarningUnresolved))

		_, warnings = buildPathChangeRewritePlan(
			"/docs/source",
			"/docs/moved",
			MarkdownSourceKindPage,
			nil,
			[]rewriteCandidate{{Destination: "broken.md"}},
			[]markdownlinks.InlineDestination{{Destination: "broken.md"}},
		)
		Expect(warnings).To(ConsistOf(testmatchers.HaveMessageID(rewriteWarningUnresolved)))
		restoreResolve()

		_, warnings = buildPathChangeRewritePlan(
			"/docs/source",
			"/docs/moved",
			MarkdownSourceKindPage,
			nil,
			[]rewriteCandidate{{Destination: "reference.md"}},
			nil,
		)
		Expect(warnings).To(ConsistOf(testmatchers.HaveMessageID(rewriteWarningUnsupportedSyntax)))

		Expect(normalizeCandidateDestination("")).To(BeEmpty())

		_, changed, warning = rewriteLinkDestination("/docs/source", MarkdownSourceKindPage, "old", []RewriteRule{{
			OldPath: "/docs/old",
			NewPath: "/",
		}}, "")
		Expect(changed).To(BeFalse())
		Expect(warning).NotTo(BeNil())
		Expect(warning).To(testmatchers.HaveMessageID(rewriteWarningEmptyDestination))

		Expect(addMarkdownLinkRootPrefix("/", "wiki")).To(Equal("/wiki"))
		Expect(stripMarkdownLinkRootPrefix("/wiki", "wiki")).To(Equal("/"))

		restoreRel := setLinksSeam(&linksFilepathRel, func(string, string) (string, error) {
			return "", errors.New("relative path failed")
		})
		Expect(relativeMarkdownFileLinkPath("/docs/source", "/docs/target")).To(Equal(filepath.ToSlash("docs/target.md")))
		Expect(relativeMarkdownDestinationForSource("/docs/source", MarkdownSourceKindPage, "/docs/target", true)).To(Equal(filepath.ToSlash("docs/target.md")))
		restoreRel()

		Expect(resolveMarkdownRoutePathForSource("docs/source.md", "")).To(BeEmpty())
		Expect(relativeMarkdownDestinationForSource("/docs/source", MarkdownSourceKindPage, "", false)).To(BeEmpty())
		Expect(applyReplacements("unchanged", nil)).To(Equal("unchanged"))
		Expect(relativeWikiLinkPath("/docs/source", "")).To(BeEmpty())
	})

	It("covers remaining relative path-change rewrite branches", func() {
		restoreResolve := setLinksSeam(&linksResolveMarkdownRoutePath, func(tree.MarkdownPath, string) (tree.RoutePath, error) {
			return "", errors.New("resolve failed")
		})
		rewritten, changed, warning := rewriteRelativeLinkForPathChange(
			"/docs/source",
			"/docs/moved",
			MarkdownSourceKindPage,
			"../../../escape.md",
			nil,
		)
		Expect(rewritten).To(Equal("../../../escape.md"))
		Expect(changed).To(BeFalse())
		Expect(warning).NotTo(BeNil())
		Expect(warning).To(testmatchers.HaveMessageID(rewriteWarningUnresolved))
		restoreResolve()

		rewritten, changed, warning = rewriteRelativeLinkForPathChange(
			"/docs/source",
			"/docs/moved",
			MarkdownSourceKindPage,
			"old.md",
			[]RewriteRule{{
				OldPath:    "/docs/old",
				NewPath:    "/docs/new-section",
				OutputKind: TargetKindSection,
			}},
		)
		Expect(warning).To(BeNil())
		Expect(changed).To(BeTrue())
		Expect(rewritten).To(Equal("new-section"))

		rewritten, changed, warning = rewriteRelativeLinkForPathChange(
			"/docs/source",
			"/docs/source",
			MarkdownSourceKindPage,
			"target.md",
			nil,
		)
		Expect(warning).To(BeNil())
		Expect(changed).To(BeFalse())
		Expect(rewritten).To(Equal("target.md"))

		rewritten, changed, warning = rewriteRelativeLinkForPathChange(
			"/docs/source",
			"/docs/moved",
			MarkdownSourceKindPage,
			"old",
			[]RewriteRule{{
				OldPath: "/docs/old",
				NewPath: "/",
			}},
		)
		Expect(rewritten).To(Equal("old"))
		Expect(changed).To(BeFalse())
		Expect(warning).NotTo(BeNil())
		Expect(warning).To(testmatchers.HaveMessageID(rewriteWarningEmptyDestination))
	})

	It("covers remaining service-level error and no-op branches", func() {
		unloadedTree := tree.NewTreeService(GinkgoT().TempDir())
		Expect(NewLinkService("", unloadedTree, newSQLClosedLinksStore()).IndexAllPages()).To(Succeed())

		loadedTree := newLoadedLinksTreeService()
		Expect(NewLinkService("", loadedTree, newSQLClosedLinksStore()).IndexAllPages()).To(HaveOccurred())

		page := createLoadedLinksPage(loadedTree, "Missing Content", "missing-content", "[Target](target.md)")
		contentPath := filepath.Join(loadedTree.RootDir(), page.CalculateRoutePath().MarkdownContentPath(page.Kind).FilesystemPath())
		Expect(os.Remove(contentPath)).To(Succeed())
		Expect(NewLinkService("", loadedTree, newAdditionalLinksStore()).IndexAllPages()).To(HaveOccurred())

		Expect(rewriteResolvedTargets("/docs/source", tree.NodeKindPage, nil, nil, loadedTree, markdownLinkIndexForTree(loadedTree))).To(BeNil())
	})
})

func markdownlinksResolution(code markdownlinks.IssueCode) markdownlinks.Resolution {
	return markdownlinks.Resolution{Code: code}
}

func newLoadedLinksTreeService() *tree.TreeService {
	GinkgoHelper()

	treeService := tree.NewTreeService(GinkgoT().TempDir())
	Expect(treeService.LoadTree()).To(Succeed())
	return treeService
}

func createLoadedLinksPage(treeService *tree.TreeService, title string, slug string, content string) *tree.Page {
	GinkgoHelper()

	kind := tree.NodeKindPage
	pageID, err := treeService.CreateNode(newFixtureUserID("links-tester"), nil, title, newFixtureSlug(slug), &kind)
	Expect(err).NotTo(HaveOccurred())
	Expect(pageID).NotTo(BeNil())
	Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("links-tester"), *pageID, title, newFixtureSlug(slug), &content, false)).To(Succeed())

	page, err := treeService.GetPage(*pageID)
	Expect(err).NotTo(HaveOccurred())
	return page
}

func newSQLClosedLinksStore() *LinksStore {
	GinkgoHelper()

	store, err := NewLinksStore(GinkgoT().TempDir())
	Expect(err).NotTo(HaveOccurred())
	Expect(store.db.Close()).To(Succeed())
	DeferCleanup(func() {
		_ = store.Close()
	})
	return store
}

func setLinksSeam[T any](target *T, replacement T) func() {
	GinkgoHelper()

	original := *target
	*target = replacement
	restored := false
	restore := func() {
		if restored {
			return
		}
		*target = original
		restored = true
	}
	DeferCleanup(restore)
	return restore
}

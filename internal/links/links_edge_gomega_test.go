package links

import (
	"errors"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = Describe("links persistence and rewrite edge behavior", func() {
	It("migrates a legacy links table without target kinds", Label("integration"), func() {
		store := newManualLinksStore(linksTempDir())
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
		Expect(linksTableSchemaFor(columns)).To(matchKindAwareLinksTableSchema())
		Expect(store).To(HaveOutgoingKindsForPage(newFixturePageID("legacy-source"), defaultStoredTargetKind))
	})

	It("migrates a legacy links table with non-key target kind values", Label("integration"), func() {
		store := newManualLinksStore(linksTempDir())
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

	It("returns unfiltered prefix matches and marks broken links through service wrappers", Label("integration"), func() {
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
		Expect(brokenPage).To(ConsistOf(matchBrokenStoredBacklink(newFixturePageID("source-page"))))

		Expect(store.HealLinksForPath("/docs/topic", newFixturePageID("healed-page"))).To(Succeed())
		healed, err := store.GetBacklinksForPage(newFixturePageID("healed-page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(healed).To(ConsistOf(matchHealedStoredBacklink(
			newFixturePageID("source-page"),
			newFixturePageID("healed-page"),
			TargetKindPage,
		)))

		Expect(service.MarkLinksBrokenForPath("docs/topic")).To(Succeed())
		allBroken, err := store.GetBrokenIncomingForPath("/docs/topic")
		Expect(err).NotTo(HaveOccurred())
		Expect(allBroken).To(HaveLen(2))

		Expect(service.MarkLinksBrokenForPrefix("docs/topic")).To(Succeed())
		descendant, err := store.GetBrokenIncomingForPath("/docs/topic/child")
		Expect(err).NotTo(HaveOccurred())
		Expect(descendant).To(HaveLen(1))
	})

	It("closes stores and exposes the underlying database handle defensively", Label("integration"), func() {
		store := newAdditionalLinksStore()
		service := NewLinkService("", nil, store)

		Expect(store.GetDB()).NotTo(BeNil())
		Expect(service.Close()).To(Succeed())
		Expect(store.GetDB()).To(BeNil())
		Expect(service.Close()).To(Succeed())
		Expect(NewLinkService("", nil, nil).Close()).To(Succeed())
	})

	It("normalizes wiki paths, suffixes, target kinds, and relative destinations", Label("unit"), func() {
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

		Expect(wikiDestinationExtensionFor("docs/page")).To(Equal(wikiDestinationExtensionless))
		Expect(wikiDestinationExtensionFor("docs/page/")).To(Equal(wikiDestinationCanonicalOrNonWiki))
		Expect(wikiDestinationExtensionFor("docs/page.md?x=1")).To(Equal(wikiDestinationCanonicalOrNonWiki))

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

	It("deduplicates rewrite warnings and reports rewrite-rule application results", Label("unit"), func() {
		warnings := dedupeWarnings([]RewriteWarning{
			{Message: "same"},
			{Message: "same"},
			{Message: "different"},
		})
		Expect(warnings).To(Equal([]RewriteWarning{
			{Message: "same"},
			{Message: "different"},
		}))

		Expect(applyRewriteRulesResult("/docs/old", []RewriteRule{{
			OldPath: "/docs/old",
			NewPath: "/docs/new",
		}})).To(matchAppliedRewriteRule(tree.RoutePathFromString("/docs/new").Clean()))

		Expect(applyRewriteRulesForKindResult("/docs/old/child", TargetKindPage, []RewriteRule{{
			OldPath: "/docs/old",
			NewPath: "/docs/new",
			Kind:    TargetKindPage,
		}})).To(matchUnappliedRewriteRule())

		Expect(applyRewriteRulesForKindResult("/docs/old/child", TargetKindPage, []RewriteRule{{
			OldPath:    "/docs/old",
			NewPath:    "/docs/new",
			OutputKind: TargetKindPage,
		}})).To(matchUnappliedRewriteRule())

		Expect(rewriteRuleKindEligibilityFor(RewriteRule{Kind: TargetKindPage}, "")).To(Equal(rewriteRuleKindRejected))
	})

	It("uses the in-memory markdown index fallback for loaded trees", Label("integration"), func() {
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

	It("returns empty fallbacks for unloaded tree resolution and missing result targets", Label("integration"), func() {
		unloadedTree := tree.NewTreeService(linksTempDir())

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

	It("propagates store failures through service operations without nil-store panics", Label("integration"), func() {
		loadedTree := newLoadedLinksTreeService()
		page := createLoadedLinksPage(loadedTree, "Source", "source", "[Target](target.md)")

		clearErr := errors.New("links service clear failed")
		clearService := NewLinkService("", loadedTree, linksStoreWithExecError(clearErr))
		Expect(clearService.ClearLinks()).To(matchLinksError(clearErr))
		Expect(clearService.IndexAllPages()).To(matchLinksError(clearErr))

		statusErr := errors.New("links service status query failed")
		_, err := NewLinkService("", loadedTree, linksStoreWithQueryError(statusErr)).GetLinkStatusForPage(page.ID, page.CalculateRoutePath())
		Expect(err).To(matchLinksError(statusErr))

		updateErr := errors.New("links service update failed")
		Expect(NewLinkService("", loadedTree, linksStoreWithExecError(updateErr)).UpdateLinksForPage(page, page.Content)).To(matchLinksError(updateErr))

		rewriteErr := errors.New("links service rewrite query failed")
		Expect(NewLinkService("", loadedTree, linksStoreWithQueryError(rewriteErr)).UpdateRewrittenLinksAndHealForPages([]*tree.Page{page}, nil)).To(matchLinksError(rewriteErr))
	})

	It("propagates store operation failures from the database layer", Label("integration"), func() {
		pageID := newFixturePageID("source-page")
		target := tree.RoutePathFromString("/docs/target")

		schemaErr := errors.New("links schema query failed")
		Expect(linksStoreWithQueryError(schemaErr).ensureSchema()).To(matchLinksError(schemaErr))
		Expect(linksStoreWithQueryError(schemaErr).ensureLinksTable()).To(matchLinksError(schemaErr))
		_, err := linksStoreWithQueryError(schemaErr).linksTableColumns()
		Expect(err).To(matchLinksError(schemaErr))

		beginErr := errors.New("links migration begin failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{beginErr: beginErr}).migrateLinksTableToKindAware(nil)).To(matchLinksError(beginErr))

		execErr := errors.New("links store exec failed")
		Expect(linksStoreWithExecError(execErr).DeleteOutgoingLinks(pageID)).To(matchLinksError(execErr))
		Expect(linksStoreWithExecError(execErr).MarkIncomingLinksBroken(newFixturePageID("target-page"))).To(matchLinksError(execErr))
		Expect(linksStoreWithExecError(execErr).MarkLinksBrokenForPath(target)).To(matchLinksError(execErr))
		Expect(linksStoreWithExecError(execErr).MarkLinksBrokenForPathAndKind(target, tree.NodeKindPage)).To(matchLinksError(execErr))
		Expect(linksStoreWithExecError(execErr).MarkLinksBrokenForPrefix("/docs")).To(matchLinksError(execErr))
		Expect(linksStoreWithExecError(execErr).MarkLinksBrokenForPrefixAndKind("/docs", tree.NodeKindPage)).To(matchLinksError(execErr))
		Expect(linksStoreWithExecError(execErr).MarkLinksBrokenForPrefixAndKind("/docs", tree.NodeKindSection)).To(matchLinksError(execErr))
		Expect(linksStoreWithExecError(execErr).AddLinks(pageID, "Source", []TargetLink{{
			TargetPageID:   newFixturePageID("target-page"),
			TargetPagePath: target.WikiPath(),
			TargetKind:     TargetKindPage,
		}})).To(matchLinksError(execErr))

		prepareErr := errors.New("links store prepare failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			prepare: prepareErrorWhen("DELETE FROM links WHERE from_page_id", prepareErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{{
			FromPageID: pageID,
			FromTitle:  "Source",
			ToPath:     "/docs/source",
			ToKind:     tree.NodeKindPage,
		}})).To(matchLinksError(prepareErr))

		queryErr := errors.New("links store query failed")
		queryStore := linksStoreWithQueryError(queryErr)
		_, err = queryStore.GetBacklinksForPage(newFixturePageID("target-page"))
		Expect(err).To(matchLinksError(queryErr))
		_, err = queryStore.GetOutgoingLinksForPage(pageID)
		Expect(err).To(matchLinksError(queryErr))
		_, err = queryStore.GetOutgoingLinksForPages([]tree.PageID{pageID})
		Expect(err).To(matchLinksError(queryErr))
		_, err = queryStore.GetRefactorMatchesForPrefix("/docs")
		Expect(err).To(matchLinksError(queryErr))
		_, err = queryStore.GetRefactorMatchesForPrefixAndKind("/docs", tree.NodeKindPage)
		Expect(err).To(matchLinksError(queryErr))
		_, err = queryStore.GetRefactorMatchesForPrefixAndKind("/docs", tree.NodeKindSection)
		Expect(err).To(matchLinksError(queryErr))
		_, err = queryStore.GetRefactorSourcePageIDsForPrefix("/docs")
		Expect(err).To(matchLinksError(queryErr))
		_, err = queryStore.GetRefactorSourcePageIDsForPrefixAndKind("/docs", tree.NodeKindPage)
		Expect(err).To(matchLinksError(queryErr))
		_, err = queryStore.GetRefactorSourcePageIDsForPrefixAndKind("/docs", tree.NodeKindSection)
		Expect(err).To(matchLinksError(queryErr))
		_, err = queryStore.GetBrokenIncomingForPath(target)
		Expect(err).To(matchLinksError(queryErr))
		_, err = queryStore.GetBrokenIncomingForPathAndKind(target, tree.NodeKindPage)
		Expect(err).To(matchLinksError(queryErr))
	})
})

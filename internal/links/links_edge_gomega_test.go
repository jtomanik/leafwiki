package links

import (
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/markdownlinks"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
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

type appliedRewriteRuleResult struct {
	Path    tree.RoutePath
	Applied bool
}

type rewrittenLinkDestinationResult struct {
	Destination string
	Changed     bool
	Warning     *RewriteWarning
}

func applyRewriteRulesResult(destination tree.RoutePath, rules []RewriteRule) appliedRewriteRuleResult {
	path, applied := applyRewriteRules(destination, rules)
	return appliedRewriteRuleResult{Path: path, Applied: applied}
}

func applyRewriteRulesForKindResult(destination tree.RoutePath, targetKind TargetKind, rules []RewriteRule) appliedRewriteRuleResult {
	path, applied := applyRewriteRulesForKind(destination, targetKind, rules)
	return appliedRewriteRuleResult{Path: path, Applied: applied}
}

func rewriteLinkDestinationResult(sourcePath tree.RoutePath, sourceKind MarkdownSourceKind, destination string, rules []RewriteRule, rootPrefix string) rewrittenLinkDestinationResult {
	rewritten, changed, warning := rewriteLinkDestination(sourcePath, sourceKind, destination, rules, rootPrefix)
	return rewrittenLinkDestinationResult{Destination: rewritten, Changed: changed, Warning: warning}
}

func rewriteRelativeLinkForPathChangeResult(sourcePath tree.RoutePath, newSourcePath tree.RoutePath, sourceKind MarkdownSourceKind, destination string, rules []RewriteRule) rewrittenLinkDestinationResult {
	rewritten, changed, warning := rewriteRelativeLinkForPathChange(sourcePath, newSourcePath, sourceKind, destination, rules)
	return rewrittenLinkDestinationResult{Destination: rewritten, Changed: changed, Warning: warning}
}

func matchAppliedRewriteRule(path tree.RoutePath) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(result appliedRewriteRuleResult) (bool, error) {
		return result.Path == path && result.Applied, nil
	}).WithMessage("apply a rewrite rule")
}

func matchUnappliedRewriteRule() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(result appliedRewriteRuleResult) (bool, error) {
		return !result.Applied, nil
	}).WithMessage("leave a rewrite rule unapplied")
}

func matchUnchangedLinkDestination(destination string, messageID sharederrors.MessageID) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(result rewrittenLinkDestinationResult) (bool, error) {
		if result.Destination != destination || result.Changed {
			return false, nil
		}
		return testmatchers.HaveMessageID(messageID).Match(result.Warning)
	}).WithMessage("leave a link destination unchanged with warning")
}

func matchRewrittenLinkDestination(destination string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(result rewrittenLinkDestinationResult) (bool, error) {
		return result.Destination == destination &&
			result.Changed &&
			result.Warning == nil, nil
	}).WithMessage("rewrite a link destination")
}

func matchUnchangedLinkDestinationWithoutWarning(destination string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(result rewrittenLinkDestinationResult) (bool, error) {
		return result.Destination == destination &&
			!result.Changed &&
			result.Warning == nil, nil
	}).WithMessage("leave a link destination unchanged without warning")
}

func matchBrokenStoredBacklink(fromPageID tree.PageID) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(backlink Backlink) (bool, error) {
		return backlink.FromPageID == fromPageID && backlink.Broken, nil
	}).WithMessage("describe a broken stored backlink")
}

func matchHealedStoredBacklink(fromPageID tree.PageID, toPageID tree.PageID, targetKind TargetKind) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(backlink Backlink) (bool, error) {
		return backlink.FromPageID == fromPageID &&
			backlink.ToPageID == toPageID &&
			backlink.ToKind == targetKind &&
			!backlink.Broken, nil
	}).WithMessage("describe a healed stored backlink")
}

func matchTreePageContentError() types.GomegaMatcher {
	return Satisfy(func(err error) bool {
		return errors.Is(err, tree.ErrGetPageContent)
	})
}

func matchLinksError(target error) types.GomegaMatcher {
	return Satisfy(func(err error) bool {
		return errors.Is(err, target)
	})
}

func linksStoreWithExecError(err error) *LinksStore {
	GinkgoHelper()
	return newScriptedLinksStore(&linksScriptedDBScript{
		exec: func(string, []driver.NamedValue) (driver.Result, error) {
			return nil, err
		},
	})
}

func linksStoreWithQueryError(err error) *LinksStore {
	GinkgoHelper()
	return newScriptedLinksStore(&linksScriptedDBScript{
		query: func(string, []driver.NamedValue) (driver.Rows, error) {
			return nil, err
		},
	})
}

var _ = Describe("links persistence and rewrite edge behavior", func() {
	It("migrates a legacy links table without target kinds", func() {
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
		Expect(linksTableKindAware(columns)).To(BeTrue())
		Expect(linksTableHasColumn(columns, "to_kind")).To(BeTrue())
		Expect(store).To(HaveOutgoingKindsForPage(newFixturePageID("legacy-source"), defaultStoredTargetKind))
	})

	It("migrates a legacy links table with non-key target kind values", func() {
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

	It("returns unfiltered prefix matches and marks broken links through service wrappers", func() {
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

	It("closes stores and exposes the underlying database handle defensively", func() {
		store := newAdditionalLinksStore()
		service := NewLinkService("", nil, store)

		Expect(store.GetDB()).NotTo(BeNil())
		Expect(service.Close()).To(Succeed())
		Expect(store.GetDB()).To(BeNil())
		Expect(service.Close()).To(Succeed())
		Expect(NewLinkService("", nil, nil).Close()).To(Succeed())
	})

	It("normalizes wiki paths, suffixes, target kinds, and relative destinations", func() {
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

	It("deduplicates rewrite warnings and reports rewrite-rule application results", func() {
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

	It("returns empty fallbacks for unloaded tree resolution and missing result targets", func() {
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

	It("propagates store failures through service operations without nil-store panics", func() {
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

	It("propagates store operation failures from the database layer", func() {
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

	It("keeps no-op markdown rewrites stable and normalizes upward path changes", func() {
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

	It("returns broken targets and loaded-tree fallbacks for defensive markdown resolution", func() {
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

	It("reports rewrite warnings when destinations cannot be resolved or emitted", func() {
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

		Expect(rewriteLinkDestinationResult("/docs/source", MarkdownSourceKindPage, "../../../escape.md", rules, "")).To(matchUnchangedLinkDestination("../../../escape.md", rewriteWarningUnresolved))

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

		Expect(rewriteLinkDestinationResult("/docs/source", MarkdownSourceKindPage, "old", []RewriteRule{{
			OldPath: "/docs/old",
			NewPath: "/",
		}}, "")).To(matchUnchangedLinkDestination("old", rewriteWarningEmptyDestination))

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

	It("rewrites relative path-change destinations and reports unresolved moves", func() {
		restoreResolve := setLinksSeam(&linksResolveMarkdownRoutePath, func(tree.MarkdownPath, string) (tree.RoutePath, error) {
			return "", errors.New("resolve failed")
		})
		Expect(rewriteRelativeLinkForPathChangeResult(
			"/docs/source",
			"/docs/moved",
			MarkdownSourceKindPage,
			"../../../escape.md",
			nil,
		)).To(matchUnchangedLinkDestination("../../../escape.md", rewriteWarningUnresolved))
		restoreResolve()

		Expect(rewriteRelativeLinkForPathChangeResult(
			"/docs/source",
			"/docs/moved",
			MarkdownSourceKindPage,
			"old.md",
			[]RewriteRule{{
				OldPath:    "/docs/old",
				NewPath:    "/docs/new-section",
				OutputKind: TargetKindSection,
			}},
		)).To(matchRewrittenLinkDestination("new-section"))

		Expect(rewriteRelativeLinkForPathChangeResult(
			"/docs/source",
			"/docs/source",
			MarkdownSourceKindPage,
			"target.md",
			nil,
		)).To(matchUnchangedLinkDestinationWithoutWarning("target.md"))

		Expect(rewriteRelativeLinkForPathChangeResult(
			"/docs/source",
			"/docs/moved",
			MarkdownSourceKindPage,
			"old",
			[]RewriteRule{{
				OldPath: "/docs/old",
				NewPath: "/",
			}},
		)).To(matchUnchangedLinkDestination("old", rewriteWarningEmptyDestination))
	})

	It("treats unloaded trees as no-ops and propagates service store failures", func() {
		unloadedTree := tree.NewTreeService(linksTempDir())
		unloadedStoreErr := errors.New("links unloaded store should not be used")
		Expect(NewLinkService("", unloadedTree, linksStoreWithExecError(unloadedStoreErr)).IndexAllPages()).To(Succeed())

		loadedTree := newLoadedLinksTreeService()
		loadedStoreErr := errors.New("links loaded store clear failed")
		Expect(NewLinkService("", loadedTree, linksStoreWithExecError(loadedStoreErr)).IndexAllPages()).To(matchLinksError(loadedStoreErr))

		page := createLoadedLinksPage(loadedTree, "Missing Content", "missing-content", "[Target](target.md)")
		contentPath := filepath.Join(loadedTree.RootDir(), page.CalculateRoutePath().MarkdownContentPath(page.Kind).FilesystemPath())
		Expect(os.Remove(contentPath)).To(Succeed())
		Expect(NewLinkService("", loadedTree, newAdditionalLinksStore()).IndexAllPages()).To(matchTreePageContentError())

		Expect(rewriteResolvedTargets("/docs/source", tree.NodeKindPage, nil, nil, loadedTree, markdownLinkIndexForTree(loadedTree))).To(BeNil())
	})
})

func markdownlinksResolution(code markdownlinks.IssueCode) markdownlinks.Resolution {
	return markdownlinks.Resolution{Code: code}
}

func newLoadedLinksTreeService() *tree.TreeService {
	GinkgoHelper()

	treeService := tree.NewTreeService(linksTempDir())
	Expect(treeService.LoadTree()).To(Succeed())
	return treeService
}

func createLoadedLinksPage(treeService *tree.TreeService, title string, slug string, content string) *tree.Page {
	GinkgoHelper()

	kind := tree.NodeKindPage
	pageID, err := treeService.CreateNode(newFixtureUserID("links-tester"), nil, title, tree.SlugFromString(slug), &kind)
	Expect(err).NotTo(HaveOccurred())
	Expect(pageID).NotTo(BeNil())
	Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("links-tester"), *pageID, title, tree.SlugFromString(slug), &content, false)).To(Succeed())

	page, err := treeService.GetPage(*pageID)
	Expect(err).NotTo(HaveOccurred())
	return page
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

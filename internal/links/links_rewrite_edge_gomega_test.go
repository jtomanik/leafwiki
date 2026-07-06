package links

import (
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/perber/wiki/internal/core/markdownlinks"
	"github.com/perber/wiki/internal/core/tree"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = Describe("links persistence and rewrite edge behavior", func() {
	It("keeps no-op markdown rewrites stable and normalizes upward path changes", Label("unit"), func() {
		engine := NewMarkdownRefactorEngine()
		rules := []RewriteRule{{
			OldPath: newFixtureRoutePath("/docs/old"),
			NewPath: newFixtureRoutePath("/docs/new"),
		}}

		Expect(engine.Rewrite("", newFixtureRoutePath("/docs/source"), rules).Content).To(BeEmpty())
		Expect(engine.Rewrite("[No links text]", newFixtureRoutePath("/docs/source"), rules).Content).To(Equal("[No links text]"))
		Expect(engine.RewriteRelativeLinksForPathChange("", newFixtureRoutePath("/docs/old"), newFixtureRoutePath("/docs/new"), rules).Content).To(BeEmpty())
		Expect(engine.RewriteRelativeLinksForPathChange("[No links text]", newFixtureRoutePath("/docs/old"), newFixtureRoutePath("/docs/new"), rules).Content).To(Equal("[No links text]"))

		result := engine.RewriteRelativeLinksForPathChange("[Bad](../../escape.md)", newFixtureRoutePath("/docs/source"), newFixtureRoutePath("/docs/moved"), nil)
		Expect(result).To(SatisfyAll(
			matchRewriteResult(gstruct.Fields{
				"Content":  Equal("[Bad](../index.md)"),
				"Warnings": BeEmpty(),
			}),
			HaveField("Count()", Equal(1)),
		))
	})

	It("returns broken targets and loaded-tree fallbacks for defensive markdown resolution", Label("integration"), func() {
		loadedTree := newLoadedLinksTreeService()
		index := markdownlinks.NewIndex([]markdownlinks.Entry{{
			Kind:      markdownlinks.EntryKindPage,
			RoutePath: newFixtureRoutePath("/ghost").Clean(),
		}})

		targets := resolveTargetLinksWithIndex(loadedTree, index, newFixtureRoutePath("/docs/source"), tree.NodeKindPage, []string{
			"%zz",
			"/",
			"/ghost.md",
		})
		Expect(targets).To(Equal([]TargetLink{{
			TargetPagePath: "/ghost",
			TargetKind:     TargetKindPage,
			Broken:         true,
		}}))

		Expect(wikiDestinationExtensionFor("docs/page#heading")).To(Equal(wikiDestinationExtensionless))
		Expect(markdownLinkIndexForTreeWithOptions(nil, markdownlinks.Options{})).NotTo(BeNil())

		restoreIndex := setLinksSeam(&newMarkdownLinkIndexFromRoot, func(string) (*markdownlinks.Index, error) {
			return nil, errors.New("index from root failed")
		})
		Expect(markdownLinkIndexForTree(loadedTree)).NotTo(BeNil())
		restoreIndex()

		Expect(markdownLinkIndexFromLoadedTreeWithOptions(nil, markdownlinks.Options{})).NotTo(BeNil())
	})

	It("reports rewrite warnings when destinations cannot be resolved or emitted", Label("unit"), func() {
		rules := []RewriteRule{{
			OldPath: newFixtureRoutePath("/docs/old"),
			NewPath: newFixtureRoutePath("/docs/new"),
		}}
		restoreResolve := setLinksSeam(&linksResolveMarkdownRoutePath, func(tree.MarkdownPath, string) (tree.RoutePath, error) {
			return "", errors.New("resolve failed")
		})

		_, warnings := buildRewritePlan(
			newFixtureRoutePath("/docs/source"),
			MarkdownSourceKindPage,
			rules,
			[]rewriteCandidate{{Destination: "../../../escape.md"}},
			[]markdownlinks.InlineDestination{{Destination: "../../../escape.md"}},
			"",
		)
		Expect(warnings).To(ConsistOf(testmatchers.HaveMessageID(rewriteWarningUnresolved)))

		Expect(rewriteLinkDestinationResult(newFixtureRoutePath("/docs/source"), MarkdownSourceKindPage, "../../../escape.md", rules, "")).To(matchUnchangedLinkDestination("../../../escape.md", rewriteWarningUnresolved))

		_, warnings = buildPathChangeRewritePlan(
			newFixtureRoutePath("/docs/source"),
			newFixtureRoutePath("/docs/moved"),
			MarkdownSourceKindPage,
			nil,
			[]rewriteCandidate{{Destination: "broken.md"}},
			[]markdownlinks.InlineDestination{{Destination: "broken.md"}},
		)
		Expect(warnings).To(ConsistOf(testmatchers.HaveMessageID(rewriteWarningUnresolved)))
		restoreResolve()

		_, warnings = buildPathChangeRewritePlan(
			newFixtureRoutePath("/docs/source"),
			newFixtureRoutePath("/docs/moved"),
			MarkdownSourceKindPage,
			nil,
			[]rewriteCandidate{{Destination: "reference.md"}},
			nil,
		)
		Expect(warnings).To(ConsistOf(testmatchers.HaveMessageID(rewriteWarningUnsupportedSyntax)))

		Expect(normalizeCandidateDestination("")).To(BeEmpty())

		Expect(rewriteLinkDestinationResult(newFixtureRoutePath("/docs/source"), MarkdownSourceKindPage, "old", []RewriteRule{{
			OldPath: newFixtureRoutePath("/docs/old"),
			NewPath: newFixtureRoutePath("/"),
		}}, "")).To(matchUnchangedLinkDestination("old", rewriteWarningEmptyDestination))

		Expect(addMarkdownLinkRootPrefix("/", "wiki")).To(Equal("/wiki"))
		Expect(stripMarkdownLinkRootPrefix("/wiki", "wiki")).To(Equal("/"))

		restoreRel := setLinksSeam(&linksFilepathRel, func(string, string) (string, error) {
			return "", errors.New("relative path failed")
		})
		Expect(relativeMarkdownFileLinkPath(newFixtureRoutePath("/docs/source"), newFixtureRoutePath("/docs/target"))).To(Equal(filepath.ToSlash("docs/target.md")))
		Expect(relativeMarkdownDestinationForSource(newFixtureRoutePath("/docs/source"), MarkdownSourceKindPage, newFixtureRoutePath("/docs/target"), true)).To(Equal(filepath.ToSlash("docs/target.md")))
		restoreRel()

		Expect(resolveMarkdownRoutePathForSource(newFixtureMarkdownPath("docs/source.md"), "")).To(BeEmpty())
		Expect(relativeMarkdownDestinationForSource(newFixtureRoutePath("/docs/source"), MarkdownSourceKindPage, newFixtureRoutePath(""), false)).To(BeEmpty())
		Expect(applyReplacements("unchanged", nil)).To(Equal("unchanged"))
		Expect(relativeWikiLinkPath("/docs/source", "")).To(BeEmpty())
	})

	It("rewrites relative path-change destinations and reports unresolved moves", Label("unit"), func() {
		restoreResolve := setLinksSeam(&linksResolveMarkdownRoutePath, func(tree.MarkdownPath, string) (tree.RoutePath, error) {
			return "", errors.New("resolve failed")
		})
		Expect(rewriteRelativeLinkForPathChangeResult(
			newFixtureRoutePath("/docs/source"),
			newFixtureRoutePath("/docs/moved"),
			MarkdownSourceKindPage,
			"../../../escape.md",
			nil,
		)).To(matchUnchangedLinkDestination("../../../escape.md", rewriteWarningUnresolved))
		restoreResolve()

		Expect(rewriteRelativeLinkForPathChangeResult(
			newFixtureRoutePath("/docs/source"),
			newFixtureRoutePath("/docs/moved"),
			MarkdownSourceKindPage,
			"old.md",
			[]RewriteRule{{
				OldPath:    newFixtureRoutePath("/docs/old"),
				NewPath:    newFixtureRoutePath("/docs/new-section"),
				OutputKind: TargetKindSection,
			}},
		)).To(matchRewrittenLinkDestination("new-section"))

		Expect(rewriteRelativeLinkForPathChangeResult(
			newFixtureRoutePath("/docs/source"),
			newFixtureRoutePath("/docs/source"),
			MarkdownSourceKindPage,
			"target.md",
			nil,
		)).To(matchUnchangedLinkDestinationWithoutWarning("target.md"))

		Expect(rewriteRelativeLinkForPathChangeResult(
			newFixtureRoutePath("/docs/source"),
			newFixtureRoutePath("/docs/moved"),
			MarkdownSourceKindPage,
			"old",
			[]RewriteRule{{
				OldPath: newFixtureRoutePath("/docs/old"),
				NewPath: newFixtureRoutePath("/"),
			}},
		)).To(matchUnchangedLinkDestination("old", rewriteWarningEmptyDestination))
	})

	It("treats unloaded trees as no-ops and propagates service store failures", Label("integration"), func() {
		unloadedTree := tree.NewTreeService(linksTempDir())
		unloadedStoreErr := errors.New("links unloaded store should not be used")
		Expect(NewLinkService("", unloadedTree, linksStoreWithExecError(unloadedStoreErr)).IndexAllPages()).To(Succeed())

		loadedTree := newLoadedLinksTreeService()
		loadedStoreErr := errors.New("links loaded store clear failed")
		Expect(NewLinkService("", loadedTree, linksStoreWithExecError(loadedStoreErr)).IndexAllPages()).To(matchLinksError(loadedStoreErr))

		page := createLoadedLinksPage(loadedTree, "Missing Content", newFixtureSlug("missing-content"), "[Target](target.md)")
		contentPath := filepath.Join(loadedTree.RootDir(), page.CalculateRoutePath().MarkdownContentPath(page.Kind).FilesystemPath())
		Expect(os.Remove(contentPath)).To(Succeed())
		Expect(NewLinkService("", loadedTree, newAdditionalLinksStore()).IndexAllPages()).To(matchTreePageContentError())

		Expect(rewriteResolvedTargets(newFixtureRoutePath("/docs/source"), tree.NodeKindPage, nil, nil, loadedTree, markdownLinkIndexForTree(loadedTree))).To(BeNil())
	})
})

package links

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = Describe("links SQL store persistence edge behavior", func() {
	It("propagates add and replace transaction failures", Label("integration"), func() {
		fromPageID := newFixturePageID("source-page")
		targetLink := TargetLink{
			TargetPageID:   newFixturePageID("target-page"),
			TargetPagePath: "/docs/target",
			TargetKind:     TargetKindPage,
		}

		deleteErr := errors.New("links delete failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			exec: execErrorWhen(linksSQLDeleteOutgoing, deleteErr),
		}).AddLinks(fromPageID, "Source", nil)).To(MatchError(deleteErr))

		rollbackErr := errors.New("links rollback failed")
		err := newScriptedLinksStore(&linksScriptedDBScript{
			exec:        execErrorWhen(linksSQLDeleteOutgoing, deleteErr),
			rollbackErr: rollbackErr,
		}).AddLinks(fromPageID, "Source", nil)
		Expect(err).To(SatisfyAll(MatchError(deleteErr), MatchError(rollbackErr)))

		prepareInsertErr := errors.New("links prepare insert failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			prepare: prepareErrorWhen(linksSQLInsertOutgoing, prepareInsertErr),
		}).AddLinks(fromPageID, "Source", nil)).To(MatchError(prepareInsertErr))

		err = newScriptedLinksStore(&linksScriptedDBScript{
			prepare:     prepareErrorWhen(linksSQLInsertOutgoing, prepareInsertErr),
			rollbackErr: rollbackErr,
		}).AddLinks(fromPageID, "Source", nil)
		Expect(err).To(SatisfyAll(MatchError(prepareInsertErr), MatchError(rollbackErr)))

		insertErr := errors.New("links insert failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			exec: execErrorWhen(linksSQLInsertOutgoing, insertErr),
		}).AddLinks(fromPageID, "Source", []TargetLink{targetLink})).To(MatchError(insertErr))

		err = newScriptedLinksStore(&linksScriptedDBScript{
			exec:        execErrorWhen(linksSQLInsertOutgoing, insertErr),
			rollbackErr: rollbackErr,
		}).AddLinks(fromPageID, "Source", []TargetLink{targetLink})
		Expect(err).To(SatisfyAll(MatchError(insertErr), MatchError(rollbackErr)))

		restoreCloseStatement := setLinksSeam(&linksCloseStatement, func(interface{ Close() error }) error {
			return errors.New("links statement close failed")
		})
		store := newAdditionalLinksStore()
		Expect(store.AddLinks(fromPageID, "Source", nil)).To(Succeed())
		Expect(store.ReplaceLinksAndHeal(nil)).To(Succeed())
		restoreCloseStatement()

		update := PageLinkUpdate{
			FromPageID: fromPageID,
			FromTitle:  "Source",
			ToPath:     newFixtureRoutePath("/docs/source"),
			ToKind:     tree.NodeKindPage,
			Targets: []TargetLink{{
				TargetPageID:   targetLink.TargetPageID,
				TargetPagePath: targetLink.TargetPagePath,
				TargetKind:     targetLink.TargetKind,
				Broken:         true,
			}},
		}
		Expect(store.ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(Succeed())

		prepareDeleteErr := errors.New("links prepare delete failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			prepare: prepareErrorWhen(linksSQLDeleteOutgoing, prepareDeleteErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(MatchError(prepareDeleteErr))

		err = newScriptedLinksStore(&linksScriptedDBScript{
			prepare:     prepareErrorWhen(linksSQLDeleteOutgoing, prepareDeleteErr),
			rollbackErr: rollbackErr,
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})
		Expect(err).To(SatisfyAll(MatchError(prepareDeleteErr), MatchError(rollbackErr)))

		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			prepare: prepareErrorWhen(linksSQLInsertOutgoing, prepareInsertErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(MatchError(prepareInsertErr))

		prepareHealErr := errors.New("links prepare heal failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			prepare: prepareErrorWhen(linksSQLHealPageLinks, prepareHealErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(MatchError(prepareHealErr))

		prepareSectionHealErr := errors.New("links prepare section heal failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			prepare: prepareErrorWhen(linksSQLHealSectionLinks, prepareSectionHealErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(MatchError(prepareSectionHealErr))

		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			exec: execErrorWhen(linksSQLDeleteOutgoing, deleteErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(MatchError(deleteErr))

		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			exec: execErrorWhen(linksSQLInsertOutgoing, insertErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(MatchError(insertErr))

		healPageErr := errors.New("links heal page failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			exec: execErrorWhen(linksSQLHealPageLinks, healPageErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{update})).To(MatchError(healPageErr))

		sectionUpdate := update
		sectionUpdate.ToKind = tree.NodeKindSection
		healSectionErr := errors.New("links heal section failed")
		Expect(newScriptedLinksStore(&linksScriptedDBScript{
			exec: execErrorWhen(linksSQLHealSectionLinks, healSectionErr),
		}).ReplaceLinksAndHeal([]PageLinkUpdate{sectionUpdate})).To(MatchError(healSectionErr))

		beginErr := errors.New("links begin failed")
		loadedTree := newLoadedLinksTreeService()
		createLoadedLinksPage(loadedTree, "Source", newFixtureSlug("source"), "[Target](target.md)")
		service := NewLinkService("", loadedTree, newScriptedLinksStore(&linksScriptedDBScript{beginErr: beginErr}))
		Expect(service.IndexAllPages()).To(MatchError(beginErr))
	})
})

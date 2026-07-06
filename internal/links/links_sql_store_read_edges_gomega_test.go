package links

import (
	"database/sql/driver"
	"errors"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = Describe("links SQL store persistence edge behavior", func() {
	It("returns stable read results and propagates query row failures", Label("integration"), func() {
		store := newAdditionalLinksStore()
		restoreCloseRows := setLinksSeam(&linksCloseRows, func(interface{ Close() error }) error {
			return errors.New("links query close failed")
		})
		expectAllReadMethodsSucceed(store)
		restoreCloseRows()

		queryErr := errors.New("links read query failed")
		queryErrStore := newScriptedLinksStore(&linksScriptedDBScript{
			query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return nil, queryErr
			},
		})
		Expect(queryErrStore).To(HaveReadMethodsPropagate(queryErr))

		rowsErr := errors.New("links rows failed")
		rowsErrStore := newScriptedLinksStore(&linksScriptedDBScript{
			query: func(string, []driver.NamedValue) (driver.Rows, error) {
				return &linksScriptedRows{columns: []string{"bad"}, nextErr: rowsErr}, nil
			},
		})
		Expect(rowsErrStore).To(HaveReadMethodsPropagate(rowsErr))

		nullRowsStore := newScriptedLinksStore(&linksScriptedDBScript{
			query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				switch {
				case strings.Contains(query, "WHERE to_page_id"):
					return linksRows([]string{"from_page_id", "to_page_id", "from_title", "to_kind"}, []driver.Value{"source-page", nil, "Source", defaultStoredTargetKind}), nil
				case strings.Contains(query, "WHERE from_page_id"):
					return linksRows([]string{"from_page_id", "to_page_id", "to_path", "to_kind", "from_title", "broken"}, []driver.Value{"source-page", nil, "/docs/target", defaultStoredTargetKind, "Source", int64(0)}), nil
				default:
					return linksRows([]string{"from_page_id", "to_page_id", "from_title", "to_kind"}, []driver.Value{"source-page", nil, "Source", defaultStoredTargetKind}), nil
				}
			},
		})
		backlinks, err := nullRowsStore.GetBacklinksForPage(newFixturePageID("target-page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(backlinks).To(ContainElement(matchBacklink(gstruct.Fields{
			"ToPageID": BeEmpty(),
		})))
		outgoing, err := nullRowsStore.GetOutgoingLinksForPage(newFixturePageID("source-page"))
		Expect(err).NotTo(HaveOccurred())
		Expect(outgoing).To(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"ToPageID": BeEmpty(),
		})))
		broken, err := nullRowsStore.GetBrokenIncomingForPathAndKind(newFixtureRoutePath("/docs/target"), tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(broken).To(ContainElement(matchBacklink(gstruct.Fields{
			"ToPageID": BeEmpty(),
		})))

		validBrokenStore := newAdditionalLinksStore()
		Expect(validBrokenStore.AddLinks(newFixturePageID("broken-source"), "Broken Source", []TargetLink{{
			TargetPageID:   newFixturePageID("target-page"),
			TargetPagePath: "/docs/target",
			TargetKind:     TargetKindPage,
			Broken:         true,
		}})).To(Succeed())
		broken, err = validBrokenStore.GetBrokenIncomingForPathAndKind(newFixtureRoutePath("/docs/target"), tree.NodeKindPage)
		Expect(err).NotTo(HaveOccurred())
		Expect(broken).To(ConsistOf(matchBacklink(gstruct.Fields{
			"ToPageID": Equal(newFixturePageID("target-page")),
		})))

		statusTree := newLoadedLinksTreeService()
		sourcePage := createLoadedLinksPage(statusTree, "Source", newFixtureSlug("source"), "")
		targetPage := createLoadedLinksPage(statusTree, "Target", newFixtureSlug("target"), "")
		Expect(store.AddLinks(sourcePage.ID, sourcePage.Title, []TargetLink{{
			TargetPageID:   targetPage.ID,
			TargetPagePath: targetPage.CalculateRoutePath().WikiPath(),
			TargetKind:     TargetKindPage,
		}})).To(Succeed())
		status, err := NewLinkService("", statusTree, store).GetLinkStatusForPage(sourcePage.ID, sourcePage.CalculateRoutePath())
		Expect(err).NotTo(HaveOccurred())
		Expect(status).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Outgoings":       HaveLen(1),
			"BrokenOutgoings": BeEmpty(),
		})))

		brokenErr := errors.New("broken incoming failed")
		statusStore := newScriptedLinksStore(&linksScriptedDBScript{
			query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "to_kind IN") {
					return nil, brokenErr
				}
				return &linksScriptedRows{}, nil
			},
		})
		_, err = NewLinkService("", nil, statusStore).GetLinkStatusForPage(newFixturePageID("source-page"), newFixtureRoutePath("/docs/source"))
		Expect(err).To(MatchError(brokenErr))

		outgoingErr := errors.New("outgoing failed")
		statusStore = newScriptedLinksStore(&linksScriptedDBScript{
			query: func(query string, _ []driver.NamedValue) (driver.Rows, error) {
				if strings.Contains(query, "WHERE from_page_id") {
					return nil, outgoingErr
				}
				return &linksScriptedRows{}, nil
			},
		})
		_, err = NewLinkService("", nil, statusStore).GetLinkStatusForPage(newFixturePageID("source-page"), newFixtureRoutePath("/docs/source"))
		Expect(err).To(MatchError(outgoingErr))
	})
})

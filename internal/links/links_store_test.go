package links

import (
	"fmt"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("links store persistence", func() {
	ginkgo.It("creates its SQLite database inside the configured storage directory", ginkgo.Label("integration"), func() {
		tmp := linksTempDir()
		store, err := NewLinksStore(tmp)
		Expect(err).NotTo(HaveOccurred())
		closeLinksStoreForTest(store)

		Expect(filepath.Join(tmp, "links.db")).To(BeAnExistingFile())
	})

	ginkgo.It("keeps Windows-style storage paths under the links database file", ginkgo.Label("unit"), func() {
		got := strings.ReplaceAll(linksDatabasePath(`C:\wiki\data`, "links.db"), `\`, `/`)

		Expect(got).To(Equal(`C:/wiki/data/links.db`))
	})

	ginkgo.It("returns outgoing links for large page batches without dropping entries", ginkgo.Label("integration"), func() {
		store, err := NewLinksStore(linksTempDir())
		Expect(err).NotTo(HaveOccurred())
		closeLinksStoreForTest(store)

		pageIDs := make([]tree.PageID, 0, maxOutgoingLinksQueryArgs+5)
		for i := 0; i < maxOutgoingLinksQueryArgs+5; i++ {
			pageID := tree.PageIDFromString(fmt.Sprintf("page-%d", i))
			pageIDs = append(pageIDs, pageID)
			Expect(store.AddLinks(pageID, fmt.Sprintf("Title %s", pageID), []TargetLink{{
				TargetPageID:   tree.PageIDFromString(fmt.Sprintf("target-%s", pageID)),
				TargetPagePath: fmt.Sprintf("target/%s", pageID),
			}})).To(Succeed())
		}

		outgoingByPageID, err := store.GetOutgoingLinksForPages(pageIDs)
		Expect(err).NotTo(HaveOccurred())
		Expect(outgoingByPageID).To(HaveLen(len(pageIDs)))

		for _, pageID := range pageIDs {
			Expect(outgoingByPageID).To(HaveKeyWithValue(pageID, ConsistOf(
				gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
					"FromPageID": Equal(pageID),
					"ToPath": WithTransform(func(path tree.RoutePath) string {
						return path.WikiPath()
					}, Equal(fmt.Sprintf("/target/%s", pageID))),
				}),
			)))
		}
	})
})

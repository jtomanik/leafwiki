package links

import (
	ginkgo "github.com/onsi/ginkgo/v2"

	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/tree"
)

var _ = ginkgo.Describe("TestLinksStore_CreatesDatabaseInStorageDir", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		store, err := NewLinksStore(tmp)
		if err != nil {
			t.Fatalf("NewLinksStore err: %v", err)
		}
		defer closeLinksStoreForTest(store, t)

		if _, err := os.Stat(filepath.Join(tmp, "links.db")); err != nil {
			t.Fatalf("expected links.db in storage dir, got err: %v", err)
		}

	})
})

var _ = ginkgo.Describe("TestLinksDatabasePath_WindowsPath", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		got := strings.ReplaceAll(linksDatabasePath(`C:\wiki\data`, "links.db"), `\`, `/`)
		want := `C:/wiki/data/links.db`
		if got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}

	})
})

var _ = ginkgo.Describe("TestLinksStore_GetOutgoingLinksForPages_BatchesLargeInputs", func() {
	ginkgo.It("preserves behavior", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		store, err := NewLinksStore(tmp)
		if err != nil {
			t.Fatalf("NewLinksStore err: %v", err)
		}
		defer closeLinksStoreForTest(store, t)

		pageIDs := make([]tree.PageID, 0, maxOutgoingLinksQueryArgs+5)
		for i := 0; i < maxOutgoingLinksQueryArgs+5; i++ {
			pageID := newFixturePageID(fmt.Sprintf("page-%d", i))
			pageIDs = append(pageIDs, pageID)
			if err := store.AddLinks(pageID, fmt.Sprintf("Title %s", pageID), []TargetLink{{
				TargetPageID:   newFixturePageID(fmt.Sprintf("target-%s", pageID)),
				TargetPagePath: fmt.Sprintf("target/%s", pageID),
			}}); err != nil {
				t.Fatalf("AddLinks(%s) failed: %v", pageID, err)
			}
		}

		outgoingByPageID, err := store.GetOutgoingLinksForPages(pageIDs)
		if err != nil {
			t.Fatalf("GetOutgoingLinksForPages failed: %v", err)
		}
		if len(outgoingByPageID) != len(pageIDs) {
			t.Fatalf("expected %d page entries, got %d", len(pageIDs), len(outgoingByPageID))
		}

		for _, pageID := range pageIDs {
			outgoings := outgoingByPageID[pageID]
			if len(outgoings) != 1 {
				t.Fatalf("expected 1 outgoing for %s, got %d", pageID, len(outgoings))
			}
			if outgoings[0].FromPageID != pageID {
				t.Fatalf("expected outgoing from %s, got %s", pageID, outgoings[0].FromPageID)
			}
			if wantPath := fmt.Sprintf("/target/%s", pageID); outgoings[0].ToPath.WikiPath() != wantPath {
				t.Fatalf("expected target path %q, got %q", wantPath, outgoings[0].ToPath.WikiPath())
			}
		}

	})
})

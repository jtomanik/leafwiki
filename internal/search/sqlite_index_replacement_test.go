package search

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/perber/wiki/internal/core/tree"
)

const (
	legacySearchPath     = "docs/legacy"
	legacySearchFilePath = "docs/legacy.md"
	currentSearchPath    = "docs/current"
	currentSearchFile    = "docs/current.md"
)

var _ = ginkgo.Describe("SQLite search index replacement", ginkgo.Label("integration"), func() {
	ginkgo.It("replaces stale file path and content rows for the same page ID", func() {
		index := newSQLiteIndexForSpec()
		pageID := newFixturePageID("replace-page")

		Expect(index.IndexPage(
			legacySearchPath,
			legacySearchFilePath,
			pageID,
			"Legacy indexed page",
			tree.NodeKindPage,
			"Legacy searchable body.",
		)).To(Succeed())
		Expect(index.IndexPage(
			currentSearchPath,
			currentSearchFile,
			pageID,
			"Current indexed page",
			tree.NodeKindPage,
			"Current searchable body.",
		)).To(Succeed())

		legacyIDs, err := index.SearchPageIDs("legacy", nil)
		Expect(err).To(Succeed())
		Expect(legacyIDs).To(BeEmpty())

		currentIDs, err := index.SearchPageIDs("current", nil)
		Expect(err).To(Succeed())
		Expect(currentIDs).To(Equal([]tree.PageID{pageID}))

		removed, err := index.RemovePageByFilePath(legacySearchFilePath)
		Expect(err).To(Succeed())
		Expect(removed).To(BeZero())

		removed, err = index.RemovePageByFilePath(currentSearchFile)
		Expect(err).To(Succeed())
		Expect(removed).To(Equal(int64(1)))

		currentIDs, err = index.SearchPageIDs("current", nil)
		Expect(err).To(Succeed())
		Expect(currentIDs).To(BeEmpty())
	})
})

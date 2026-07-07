package mcp

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

var _ = Describe("Semantic helper types", Label("unit"), func() {
	It("preserves page identifiers as typed page IDs", func() {
		pageID, err := exactlyOneIDOrPageID(newFixturePageID("page-1").MetadataValue(), "")
		Expect(err).NotTo(HaveOccurred())

		var semanticPageID tree.PageID = pageID
		Expect(semanticPageID).To(Equal(newFixturePageID("page-1")))
	})

	It("requires exactly one page identifier at MCP request boundaries", func() {
		legacyID := newFixturePageID("legacy-page").MetadataValue()
		pageID := newFixturePageID("semantic-page").MetadataValue()

		got, err := exactlyOneIDOrPageID("", pageID)
		Expect(err).To(Succeed())
		Expect(got).To(Equal(newFixturePageID("semantic-page")))

		_, err = exactlyOneIDOrPageID(legacyID, pageID)
		Expect(err).To(matchLocalizedErrorCode(
			errCodeMCPPageIdentifierAmbiguous,
			sharederrors.MessageIDForCode(errCodeMCPPageIdentifierAmbiguous),
		))

		_, err = exactlyOneIDOrPageID("", "")
		Expect(err).To(matchLocalizedErrorCode(
			errCodeMCPPageIdentifierRequired,
			sharederrors.MessageIDForCode(errCodeMCPPageIdentifierRequired),
		))
	})

	It("normalizes page route and kind wire values before page lookup", func() {
		rawInstallPath := "guides/install"
		routePath, kind, err := normalizeToolPagePathInput(
			paddedMCPRoutePath(rawInstallPath),
			mcpNodeKindWireValue(tree.NodeKindSection),
		)
		Expect(err).To(Succeed())
		Expect(routePath).To(Equal(newFixtureRoutePath("guides/install")))
		Expect(kind).To(Equal(tree.NodeKindSection))

		Expect(normalizeToolRoutePath(paddedMCPRoutePath(rawInstallPath))).To(Equal(rawInstallPath))
	})

	It("converts optional and repeated page identifiers into typed page IDs", func() {
		parentID := newFixturePageID("parent-page").MetadataValue()

		Expect(mcpPageIDPtr(nil)).To(BeNil())
		typedParentID := mcpPageIDPtr(&parentID)
		Expect(typedParentID).NotTo(BeNil())
		Expect(*typedParentID).To(Equal(newFixturePageID("parent-page")))
		Expect(mcpPageIDs([]string{
			newFixturePageID("first-page").MetadataValue(),
			newFixturePageID("second-page").MetadataValue(),
		})).To(Equal([]tree.PageID{
			newFixturePageID("first-page"),
			newFixturePageID("second-page"),
		}))
	})
})

func paddedMCPRoutePath(rawPath string) string {
	return " /" + rawPath + "/ "
}

func mcpNodeKindWireValue(kind tree.NodeKind) string {
	return fmt.Sprint(kind)
}

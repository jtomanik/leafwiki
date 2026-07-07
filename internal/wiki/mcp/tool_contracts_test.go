package mcp

import (
	"encoding/json"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/localization"
	wikipages "github.com/perber/wiki/internal/wiki/pages"
)

var _ = Describe("Tool descriptor contracts", Label("unit"), func() {
	It("uses typed IDs and description IDs", func() {
		Expect(toolMovePage).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Name":          Equal(ToolMovePage),
			"DescriptionID": Equal(ToolDescriptionMovePage),
		}))
		Expect(toolNames([]ToolDescriptor{toolMovePage})).To(Equal([]string{"wiki_move_page"}))
	})

	It("renders descriptions from the catalog", func() {
		descriptor := newToolDescriptor(ToolMovePage)
		Expect(descriptor.Description).To(Equal("Move a page to a new parent"))
	})

	It("keeps every tool descriptor description catalog-backed", func() {
		for _, descriptor := range allToolDescriptors() {
			rendered := localization.English.Render(descriptor.DescriptionID, "fallback")
			Expect(rendered).To(matchCatalogMessageRender(), "%s description ID %s should be catalog-backed", descriptor.Name, descriptor.DescriptionID)
		}
	})

	It("serializes message outputs with stable message IDs", func() {
		output := newMessageOutput(ToolMessageMovePageSuccess)
		rendered := localization.English.Render(ToolMessageMovePageSuccess, "")
		Expect(rendered).To(matchCatalogMessageRender())

		encoded, err := json.Marshal(output)
		Expect(err).NotTo(HaveOccurred())
		var decoded messageOutput
		Expect(json.Unmarshal(encoded, &decoded)).To(Succeed())
		Expect(decoded).To(matchMessageOutput(ToolMessageMovePageSuccess))
	})

	It("exposes message IDs in message-only output schemas", func() {
		schema := toolOutputSchema(ToolMovePage)
		Expect(schema).NotTo(BeNil())
		Expect(schema.Properties).To(HaveKey("message"))
		Expect(schema.Properties).To(HaveKey("messageId"))
	})

	It("keeps raw generic MCP tool errors out of the user-facing message", func() {
		rawErr := errors.New("sqlite raw private failure")

		result := mcpToolErrorResult(rawErr)
		Expect(result.Meta).To(matchMCPToolErrorMeta(errCodeMCPToolError))
	})

	It("projects localized tool errors into the MCP error envelope", func() {
		err := sharederrors.NewLocalizedErrorFromCode(errCodeMCPPageIdentifierRequired, nil)

		result := mcpToolErrorResult(err)

		Expect(result.Meta).To(matchMCPToolErrorDetail(errCodeMCPPageIdentifierRequired))
	})

	It("projects page-domain errors into the MCP error envelope", func() {
		result := mcpToolErrorResult(tree.ErrPageNotFound)

		Expect(result.Meta).To(matchMCPToolErrorDetail(wikipages.ErrCodePageNotFound))
	})
})

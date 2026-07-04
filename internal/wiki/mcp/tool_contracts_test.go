package mcp

import (
	"encoding/json"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/localization"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

var _ = Describe("Tool descriptor contracts", func() {
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
		Expect(decoded).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"MessageID": Equal(ToolMessageMovePageSuccess),
			"Message":   Not(BeEmpty()),
		}))
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
		Expect(result.Meta).To(HaveKeyWithValue("error", SatisfyAll(
			testmatchers.HaveMCPStructuredError(errCodeMCPToolError, sharederrors.MessageIDForCode(errCodeMCPToolError)),
			HaveKeyWithValue("args", HaveLen(1)),
		)))
	})
})

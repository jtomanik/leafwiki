package mcp

import (
	"encoding/json"
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/localization"
)

var _ = Describe("Tool descriptor contracts", func() {
	It("uses typed IDs and description IDs", func() {
		Expect(toolMovePage.Name).To(Equal(ToolMovePage))
		Expect(toolMovePage.DescriptionID).To(Equal(ToolDescriptionMovePage))
		Expect(toolNames([]ToolDescriptor{toolMovePage})).To(Equal([]string{"wiki_move_page"}))
	})

	It("renders descriptions from the catalog", func() {
		descriptor := newToolDescriptor(ToolMovePage)
		Expect(descriptor.Description).To(Equal("Move a page to a new parent"))
	})

	It("keeps every tool descriptor description catalog-backed", func() {
		for _, descriptor := range allToolDescriptors() {
			rendered := localization.English.Render(descriptor.DescriptionID, "fallback")
			Expect(rendered.Missing).To(BeFalse(), "%s description ID %s should be catalog-backed", descriptor.Name, descriptor.DescriptionID)
			Expect(rendered.Err).NotTo(HaveOccurred(), "%s description ID %s should render", descriptor.Name, descriptor.DescriptionID)
		}
	})

	It("serializes message outputs with stable message IDs", func() {
		output := newMessageOutput(ToolMessageMovePageSuccess)
		Expect(output.Message).To(Equal("Page moved"))

		encoded, err := json.Marshal(output)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(encoded)).To(Equal(`{"messageId":"mcp.tools.wiki_move_page.success","message":"Page moved"}`))
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
		errorMeta, ok := result.Meta["error"].(map[string]any)
		Expect(ok).To(BeTrue(), "Meta error = %#v, want map", result.Meta["error"])
		Expect(errorMeta["code"]).To(Equal(errCodeMCPToolError))
		Expect(fmt.Sprint(errorMeta["messageId"])).To(Equal("errors.mcp.tool_error"))
		Expect(errorMeta["message"]).To(Equal("MCP tool failed"))
		Expect(errorMeta["args"]).To(Equal([]string{rawErr.Error()}))
	})
})

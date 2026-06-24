package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/perber/wiki/internal/localization"
)

func TestToolDescriptorUsesTypedIDsAndDescriptionID(t *testing.T) {
	t.Parallel()

	if toolMovePage.Name != ToolMovePage {
		t.Fatalf("toolMovePage.Name = %q, want %q", toolMovePage.Name, ToolMovePage)
	}
	if toolMovePage.DescriptionID != ToolDescriptionMovePage {
		t.Fatalf("toolMovePage.DescriptionID = %q, want %q", toolMovePage.DescriptionID, ToolDescriptionMovePage)
	}
	if got := toolNames([]ToolDescriptor{toolMovePage}); len(got) != 1 || got[0] != "wiki_move_page" {
		t.Fatalf("toolNames = %#v, want serialized wiki_move_page", got)
	}
}

func TestToolDescriptorDescriptionRendersFromCatalog(t *testing.T) {
	t.Parallel()

	descriptor := newToolDescriptor(ToolMovePage)

	if descriptor.Description != "Move a page to a new parent" {
		t.Fatalf("Description = %q, want catalog-rendered move description", descriptor.Description)
	}
}

func TestAllToolDescriptorDescriptionsAreCatalogBacked(t *testing.T) {
	t.Parallel()

	for _, descriptor := range allToolDescriptors() {
		rendered := localization.English.Render(descriptor.DescriptionID, "fallback")
		if rendered.Missing || rendered.Err != nil {
			t.Fatalf("%s description ID %s is not catalog-backed: %#v", descriptor.Name, descriptor.DescriptionID, rendered)
		}
	}
}

func TestMessageOutputSerializesStableMessageID(t *testing.T) {
	t.Parallel()

	output := newMessageOutput(ToolMessageMovePageSuccess)
	if output.Message != "Page moved" {
		t.Fatalf("Message = %q, want catalog-rendered Page moved", output.Message)
	}

	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("marshal message output: %v", err)
	}

	want := `{"messageId":"mcp.tools.wiki_move_page.success","message":"Page moved"}`
	if string(encoded) != want {
		t.Fatalf("json = %s, want %s", encoded, want)
	}
}

func TestMessageOnlyToolSchemasExposeMessageID(t *testing.T) {
	t.Parallel()

	schema := toolOutputSchema(ToolMovePage)
	if schema == nil {
		t.Fatalf("toolOutputSchema(%q) is nil", ToolMovePage)
	}
	if _, ok := schema.Properties["message"]; !ok {
		t.Fatalf("schema properties = %#v, want message", schema.Properties)
	}
	if _, ok := schema.Properties["messageId"]; !ok {
		t.Fatalf("schema properties = %#v, want messageId", schema.Properties)
	}
}

func TestGenericMCPToolErrorDoesNotRenderRawErrorAsMessage(t *testing.T) {
	t.Parallel()

	rawErr := errors.New("sqlite raw private failure")

	result, ok := mcpToolErrorResult(rawErr)
	if !ok {
		t.Fatalf("mcpToolErrorResult returned ok=false")
	}
	errorMeta, ok := result.Meta["error"].(map[string]any)
	if !ok {
		t.Fatalf("Meta error = %#v, want map", result.Meta["error"])
	}
	if errorMeta["code"] != errCodeMCPToolError {
		t.Fatalf("code = %#v, want %q", errorMeta["code"], errCodeMCPToolError)
	}
	if fmt.Sprint(errorMeta["messageId"]) != "errors.mcp.tool_error" {
		t.Fatalf("messageId = %#v, want errors.mcp.tool_error", errorMeta["messageId"])
	}
	if errorMeta["message"] != "MCP tool failed" {
		t.Fatalf("message = %#v, want stable catalog message", errorMeta["message"])
	}
	args, ok := errorMeta["args"].([]string)
	if !ok || len(args) != 1 || args[0] != rawErr.Error() {
		t.Fatalf("args = %#v, want raw error detail preserved separately", errorMeta["args"])
	}
}

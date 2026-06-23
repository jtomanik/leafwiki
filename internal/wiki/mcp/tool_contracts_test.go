package mcp

import (
	"encoding/json"
	"testing"
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

func TestMessageOutputSerializesStableMessageID(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(messageOutput{
		MessageID: ToolMessageMovePageSuccess,
		Message:   "Page moved",
	})
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

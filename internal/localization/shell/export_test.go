package shell

import (
	"strings"
	"testing"
)

func TestRunMessagesExportMatchesGeneratedFile(t *testing.T) {
	t.Parallel()

	generated, err := GenerateRunMessages()
	if err != nil {
		t.Fatalf("GenerateRunMessages: %v", err)
	}

	if !strings.Contains(generated, "LEAFWIKI_RUN_MSG_USAGE='Usage: scripts/run.sh <mcp|agent-hook> [options]'") {
		t.Fatalf("generated run messages missing usage variable:\n%s", generated)
	}
	if !strings.Contains(generated, "LEAFWIKI_RUN_MSG_HELP_BODY='Modes:") {
		t.Fatalf("generated run messages missing help body variable:\n%s", generated)
	}
	for _, variable := range []string{
		"LEAFWIKI_RUN_MSG_ERROR_PREFIX='Error:'",
		"LEAFWIKI_RUN_MSG_DRY_RUN_MCP_CONFIG='Would run LeafWiki with YAML config for MCP'",
		"LEAFWIKI_RUN_MSG_DRY_RUN_MCP_NATIVE='Would run LeafWiki native MCP STDIO'",
		"LEAFWIKI_RUN_MSG_DRY_RUN_STDIO_ATTACH='STDIO attach:",
		"LEAFWIKI_RUN_MSG_DRY_RUN_AGENT_HOOK='Would run LeafWiki agent hook'",
		"LEAFWIKI_RUN_MSG_DRY_RUN_HTTP_CONFIG='HTTP UI: configured by'",
		"LEAFWIKI_RUN_MSG_DRY_RUN_HTTP_URL='HTTP UI:'",
	} {
		if !strings.Contains(generated, variable) {
			t.Fatalf("generated run messages missing %q:\n%s", variable, generated)
		}
	}
	if err := ValidateGeneratedRunMessages("../../../scripts/run_messages.sh"); err != nil {
		t.Fatalf("ValidateGeneratedRunMessages: %v", err)
	}
}

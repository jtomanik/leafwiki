package shell

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/localization"
)

var _ = Describe("run message export", func() {
	It("TestRunMessagesExportMatchesGeneratedFile", func() {
		generated, err := GenerateRunMessages()
		Expect(err).NotTo(HaveOccurred())

		Expect(generated).To(ContainSubstring("LEAFWIKI_RUN_MSG_USAGE='Usage: scripts/run.sh <mcp|agent-hook> [options]'"))
		Expect(generated).To(ContainSubstring("LEAFWIKI_RUN_MSG_HELP_BODY='Modes:"))
		for _, variable := range []string{
			"LEAFWIKI_RUN_MSG_ERROR_PREFIX='Error:'",
			"LEAFWIKI_RUN_MSG_DRY_RUN_MCP_CONFIG='Would run LeafWiki with YAML config for MCP'",
			"LEAFWIKI_RUN_MSG_DRY_RUN_MCP_NATIVE='Would run LeafWiki native MCP STDIO'",
			"LEAFWIKI_RUN_MSG_DRY_RUN_STDIO_ATTACH='STDIO attach:",
			"LEAFWIKI_RUN_MSG_DRY_RUN_AGENT_HOOK='Would run LeafWiki agent hook'",
			"LEAFWIKI_RUN_MSG_DRY_RUN_HTTP_CONFIG='HTTP UI: configured by'",
			"LEAFWIKI_RUN_MSG_DRY_RUN_HTTP_URL='HTTP UI:'",
		} {
			Expect(generated).To(ContainSubstring(variable))
		}
		Expect(ValidateGeneratedRunMessages("../../../scripts/run_messages.sh")).To(Succeed())
	})
})

var _ = Describe("run message export edge coverage", func() {
	It("shellSingleQuote escapes single quotes for shell variables", func() {
		Expect(shellSingleQuote("can't stop")).To(Equal(`can'"'"'t stop`))
	})

	It("GenerateRunMessages writes a deterministic generated header and ordering", func() {
		generated, err := GenerateRunMessages()
		Expect(err).NotTo(HaveOccurred())

		Expect(generated).To(HavePrefix("#!/usr/bin/env bash\n# Generated from internal/localization/locales/active.en.toml.\n\n"))
		Expect(strings.Index(generated, "LEAFWIKI_RUN_MSG_USAGE=")).To(BeNumerically("<", strings.Index(generated, "LEAFWIKI_RUN_MSG_HELP_BODY=")))
		Expect(strings.Index(generated, "LEAFWIKI_RUN_MSG_HELP_BODY=")).To(BeNumerically("<", strings.Index(generated, "LEAFWIKI_RUN_MSG_ERROR_PREFIX=")))
	})

	It("GenerateRunMessages includes newer error variables used by run.sh", func() {
		generated, err := GenerateRunMessages()
		Expect(err).NotTo(HaveOccurred())

		for _, variable := range []string{
			"LEAFWIKI_RUN_MSG_ERROR_AGENT_HOOK_REQUIRES_PROVIDER=",
			"LEAFWIKI_RUN_MSG_ERROR_DISABLE_AUTH_API_KEY_CONFLICT=",
			"LEAFWIKI_RUN_MSG_ERROR_OPTION_REQUIRES_SECRET=",
			"LEAFWIKI_RUN_MSG_ERROR_UNKNOWN_OPTION=",
		} {
			Expect(generated).To(ContainSubstring(variable))
		}
	})

	It("ValidateGeneratedRunMessages rejects stale files with a helpful error", func() {
		path := filepath.Join(GinkgoT().TempDir(), "run_messages.sh")
		Expect(os.WriteFile(path, []byte("# stale\n"), 0o600)).To(Succeed())

		err := ValidateGeneratedRunMessages(path)

		Expect(err).To(MatchError(ErrGeneratedRunMessagesFile))
	})

	It("ValidateGeneratedRunMessages returns read errors for missing files", func() {
		path := filepath.Join(GinkgoT().TempDir(), "missing.sh")

		err := ValidateGeneratedRunMessages(path)

		Expect(err).To(MatchError(fs.ErrNotExist))
	})

	It("GenerateRunMessages fails when the English renderer cannot render messages", func() {
		previous := localization.English
		localization.English = nil
		DeferCleanup(func() {
			localization.English = previous
		})

		generated, err := GenerateRunMessages()

		Expect(err).To(MatchError(ErrRunMessageRenderedEmpty))
		Expect(generated).To(BeEmpty())
	})

	It("ValidateGeneratedRunMessages returns generator errors before reading the target file", func() {
		previous := localization.English
		localization.English = nil
		DeferCleanup(func() {
			localization.English = previous
		})

		err := ValidateGeneratedRunMessages(filepath.Join(GinkgoT().TempDir(), "missing.sh"))

		Expect(err).To(MatchError(ErrRunMessageRenderedEmpty))
	})
})

package plantrace

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"strings"
)

var _ = ginkgo.Describe("repo-local LeafWiki skill documentation", func() {
	ginkgo.It("keeps the llmwiki skill installable with context-first guidance", func() {
		repoRoot := canonicalPlanRepoRoot()
		raw, err := os.ReadFile(filepath.Join(repoRoot, "skills", "llmwiki", "SKILL.md"))
		Expect(err).NotTo(HaveOccurred(), "repo-local llmwiki skill should be present")
		content := string(raw)
		Expect(content).NotTo(HavePrefix("<!-- leafwiki"))
		Expect(content).To(HavePrefix("---\nname: llmwiki\n"), "repo-local skill frontmatter = %q", firstLine(content))
		Expect(content).To(ContainSubstring("wiki_get_context"), "repo-local skill should teach the context-first workflow")

	})
})

var _ = ginkgo.Describe("workspace-root documentation links", func() {
	ginkgo.It("keeps active docs README links relative to the wiki root", func() {
		repoRoot := canonicalPlanRepoRoot()
		raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "README.md"))
		Expect(err).NotTo(HaveOccurred(), "read docs/README.md")
		content := string(raw)
		for _, forbidden := range []string{
			"(docs/workspace-sync.md)",
			"(docs/install/nginx.md)",
			"(docs/install/raspberry.md)",
			"(docs/logging.md)",
			"(CONTRIBUTING.md)",
			"(docs/mcp.md)",
			"(docs/agent-collaboration.md)",
			"(docs/canonical-markdown-links.md)",
		} {
			Expect(content).NotTo(ContainSubstring(forbidden), "active wiki root content should not use repo-root link %s", forbidden)
		}

	})
})

var _ = ginkgo.Describe("historical plan visibility", func() {
	ginkgo.It("marks duplicate visible plan copies as superseded by their canonical plans", func() {
		repoRoot := canonicalPlanRepoRoot()
		for _, tc := range []struct {
			duplicate string
			canonical string
		}{
			{duplicate: "hooks.PLAN.md", canonical: "agent_hooks.PLAN.md"},
			{duplicate: "mcp_api_keys.PLAN.md", canonical: "api_keys.PLAN.md"},
			{duplicate: "remove.sidecar.PLAN.md", canonical: "mcp_transport_unification.PLAN.md"},
			{duplicate: "stdio.PLAN.md", canonical: "native_mcp_stdio_combined.PLAN.md"},
		} {
			raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "plans", tc.duplicate))
			Expect(err).NotTo(HaveOccurred(), "read duplicate plan %s", tc.duplicate)
			content := string(raw)
			Expect(content).To(ContainSubstring("Superseded duplicate"), "%s should mark itself superseded", tc.duplicate)
			Expect(content).To(ContainSubstring(tc.canonical), "%s should point to canonical plan %s", tc.duplicate, tc.canonical)
		}

	})

	ginkgo.It("warns readers that stale MCP examples are historical", func() {
		repoRoot := canonicalPlanRepoRoot()
		raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "plans", "llm_wiki_companion_skill.PLAN.md"))
		Expect(err).NotTo(HaveOccurred(), "read llm_wiki_companion_skill.PLAN.md")
		content := string(raw)
		Expect(content).To(ContainSubstring("Historical note"))
		Expect(content).To(ContainSubstring("`--enable-mcp` examples are historical"))

	})
})

func firstLine(content string) string {
	line, _, _ := strings.Cut(content, "\n")
	return line
}

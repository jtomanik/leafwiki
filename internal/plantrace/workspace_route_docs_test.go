package plantrace

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"os"
	"path/filepath"
	"strings"
)

var _ = ginkgo.It("TestLeafWikiRepoLocalSkillRemainsInstallable", func() {
	t := ginkgo.GinkgoT()
	repoRoot := canonicalPlanRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "skills", "llmwiki", "SKILL.md"))
	if err != nil {
		t.Fatalf("repo-local llmwiki skill is missing: %v", err)
	}
	content := string(raw)
	if strings.HasPrefix(content, "<!-- leafwiki") {
		t.Fatalf("repo-local skill must use skill YAML frontmatter, not LeafWiki page metadata")
	}
	if !strings.HasPrefix(content, "---\nname: llmwiki\n") {
		t.Fatalf("repo-local skill frontmatter = %q, want YAML name frontmatter", firstLine(content))
	}
	if !strings.Contains(content, "wiki_get_context") {
		t.Fatalf("repo-local skill should teach the context-first workflow")
	}

})

var _ = ginkgo.It("TestDocsReadmeUsesWikiRootLinksForActiveRootContent", func() {
	t := ginkgo.GinkgoT()
	repoRoot := canonicalPlanRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "README.md"))
	if err != nil {
		t.Fatalf("read docs/README.md: %v", err)
	}
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
		if strings.Contains(content, forbidden) {
			t.Fatalf("docs/README.md contains repo-root link %s; active wiki root content must use wiki-root or external links", forbidden)
		}
	}

})

var _ = ginkgo.It("TestDuplicateVisiblePlanCopiesAreMarkedSuperseded", func() {
	t := ginkgo.GinkgoT()
	repoRoot := canonicalPlanRepoRoot(t)
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
		if err != nil {
			t.Fatalf("read duplicate plan %s: %v", tc.duplicate, err)
		}
		content := string(raw)
		if !strings.Contains(content, "Superseded duplicate") || !strings.Contains(content, tc.canonical) {
			t.Fatalf("%s must clearly mark itself as superseded by %s", tc.duplicate, tc.canonical)
		}
	}

})

var _ = ginkgo.It("TestHistoricalLLMWikiCompanionPlanWarnsAboutStaleMCPExamples", func() {
	t := ginkgo.GinkgoT()
	repoRoot := canonicalPlanRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(repoRoot, "docs", "plans", "llm_wiki_companion_skill.PLAN.md"))
	if err != nil {
		t.Fatalf("read llm_wiki_companion_skill.PLAN.md: %v", err)
	}
	content := string(raw)
	if !strings.Contains(content, "Historical note") || !strings.Contains(content, "`--enable-mcp` examples are historical") {
		t.Fatalf("visible historical plan must warn readers that stale --enable-mcp examples are historical")
	}

})

func firstLine(content string) string {
	line, _, _ := strings.Cut(content, "\n")
	return line
}

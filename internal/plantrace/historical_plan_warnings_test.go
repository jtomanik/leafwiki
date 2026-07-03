package plantrace

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"strings"
)

var _ = ginkgo.Describe("historical plan warnings", func() {
	ginkgo.It("marks removed workspace-sync environment examples as historical", func() {
		repoRoot := markdownLinkRootPrefixPlanRepoRoot()
		plans, err := filepath.Glob(filepath.Join(repoRoot, "docs", "plans", "*.PLAN.md"))
		Expect(err).NotTo(HaveOccurred(), "glob historical plans")

		for _, planPath := range plans {
			raw, err := os.ReadFile(planPath)
			Expect(err).NotTo(HaveOccurred(), "read historical plan %s", planPath)
			content := string(raw)
			if !strings.Contains(content, "E2E_ENABLE_WORKSPACE_SYNC=1") {
				continue
			}
			rel, relErr := filepath.Rel(repoRoot, planPath)
			if relErr != nil {
				rel = planPath
			}
			Expect(content).To(ContainSubstring("Historical note"), "%s should warn readers about removed workspace-sync env examples", rel)
			Expect(content).To(ContainSubstring("always-on Git-backed workspace sync"), "%s should explain the current sync model", rel)
		}

	})
})

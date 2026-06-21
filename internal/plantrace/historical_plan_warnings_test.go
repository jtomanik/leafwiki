package plantrace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoricalPlansWithRemovedWorkspaceSyncEnvWarnReaders(t *testing.T) {
	repoRoot := markdownLinkRootPrefixPlanRepoRoot(t)
	plans, err := filepath.Glob(filepath.Join(repoRoot, "docs", "plans", "*.PLAN.md"))
	if err != nil {
		t.Fatalf("glob historical plans: %v", err)
	}

	for _, planPath := range plans {
		raw, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatalf("read %s: %v", planPath, err)
		}
		content := string(raw)
		if !strings.Contains(content, "E2E_ENABLE_WORKSPACE_SYNC=1") {
			continue
		}
		if !strings.Contains(content, "Historical note") ||
			!strings.Contains(content, "always-on Git-backed workspace sync") {
			rel, err := filepath.Rel(repoRoot, planPath)
			if err != nil {
				rel = planPath
			}
			t.Fatalf("%s contains removed E2E_ENABLE_WORKSPACE_SYNC=1 examples without a federated-runtime cleanup historical warning", rel)
		}
	}
}

package plantrace

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/types"
)

func haveCanonicalPlanScenarioMapping() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(mapping canonicalPlanScenarioCoverage) (bool, error) {
		return mapping.title != "" && mapping.evidence.file != "" && mapping.evidence.text != "", nil
	}).WithMessage("describe a canonical plan scenario with evidence")
}

func existInCanonicalPlanEvidenceFile(repoRoot string, title string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(evidence canonicalPlanEvidence) (bool, error) {
		raw, err := os.ReadFile(filepath.Join(repoRoot, evidence.file))
		if err != nil {
			return false, fmt.Errorf("scenario %q evidence file %s cannot be read: %w", title, evidence.file, err)
		}
		return strings.Contains(string(raw), evidence.text), nil
	}).WithMessage("exist in the mapped canonical plan evidence file")
}

func canonicalPlanEvidenceForTitle(evidenceByTitle map[string]canonicalPlanEvidence, title string) (canonicalPlanEvidence, error) {
	evidence, ok := evidenceByTitle[title]
	if !ok {
		return canonicalPlanEvidence{}, fmt.Errorf("plan scenario %q has no automated-test evidence mapping", title)
	}
	return evidence, nil
}

func haveMarkdownLinkRootPrefixScenarioMapping() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(mapping markdownLinkRootPrefixScenarioCoverage) (bool, error) {
		return mapping.title != "" && mapping.evidence.file != "" && mapping.evidence.text != "", nil
	}).WithMessage("describe a markdown link root prefix scenario with evidence")
}

func existInMarkdownLinkRootPrefixEvidenceFile(repoRoot string, title string) types.GomegaMatcher {
	return gcustom.MakeMatcher(func(evidence markdownLinkRootPrefixEvidence) (bool, error) {
		raw, err := os.ReadFile(filepath.Join(repoRoot, evidence.file))
		if err != nil {
			return false, fmt.Errorf("scenario %q evidence file %s cannot be read: %w", title, evidence.file, err)
		}
		return strings.Contains(string(raw), evidence.text), nil
	}).WithMessage("exist in the mapped markdown link root prefix evidence file")
}

func markdownLinkRootPrefixEvidenceForTitle(evidenceByTitle map[string]markdownLinkRootPrefixEvidence, title string) (markdownLinkRootPrefixEvidence, error) {
	evidence, ok := evidenceByTitle[title]
	if !ok {
		return markdownLinkRootPrefixEvidence{}, fmt.Errorf("plan scenario %q has no automated-test evidence mapping", title)
	}
	return evidence, nil
}

func markdownLinkRootPrefixFocusedCommandLines(plan string) []string {
	var lines []string
	for _, line := range strings.Split(plan, "\n") {
		if strings.Contains(line, "./e2e/run.sh tests/") && strings.Contains(line, `--grep "markdown link root prefix"`) {
			lines = append(lines, line)
		}
	}
	return lines
}

func plantraceSourceFile() (string, error) {
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		return "", fmt.Errorf("runtime.Caller did not locate the plantrace source file")
	}
	return file, nil
}

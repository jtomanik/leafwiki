package plantrace

import (
	"fmt"
	"os"
	"path/filepath"
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

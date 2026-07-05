package plantrace

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
)

func haveCanonicalPlanScenarioMapping() types.GomegaMatcher {
	return WithTransform(canonicalPlanScenarioMappingStateFor, Equal(planScenarioMappingComplete))
}

func existInCanonicalPlanEvidenceFile(repoRoot string, title string) types.GomegaMatcher {
	return WithTransform(func(evidence canonicalPlanEvidence) planEvidenceReference {
		return planEvidenceReference{title: title, file: evidence.file, text: evidence.text}
	}, matchPlanEvidenceFile(repoRoot))
}

func canonicalPlanEvidenceForTitle(evidenceByTitle map[string]canonicalPlanEvidence, title string) (canonicalPlanEvidence, error) {
	evidence, ok := evidenceByTitle[title]
	if !ok {
		return canonicalPlanEvidence{}, fmt.Errorf("plan scenario %q has no automated-test evidence mapping", title)
	}
	return evidence, nil
}

func haveMarkdownLinkRootPrefixScenarioMapping() types.GomegaMatcher {
	return WithTransform(markdownLinkRootPrefixScenarioMappingStateFor, Equal(planScenarioMappingComplete))
}

func existInMarkdownLinkRootPrefixEvidenceFile(repoRoot string, title string) types.GomegaMatcher {
	return WithTransform(func(evidence markdownLinkRootPrefixEvidence) planEvidenceReference {
		return planEvidenceReference{title: title, file: evidence.file, text: evidence.text}
	}, matchPlanEvidenceFile(repoRoot))
}

func markdownLinkRootPrefixEvidenceForTitle(evidenceByTitle map[string]markdownLinkRootPrefixEvidence, title string) (markdownLinkRootPrefixEvidence, error) {
	evidence, ok := evidenceByTitle[title]
	if !ok {
		return markdownLinkRootPrefixEvidence{}, fmt.Errorf("plan scenario %q has no automated-test evidence mapping", title)
	}
	return evidence, nil
}

type planScenarioMappingState string

const (
	planScenarioMappingComplete            planScenarioMappingState = "scenario maps to repository evidence"
	planScenarioMappingMissingTitle        planScenarioMappingState = "scenario mapping is missing a plan title"
	planScenarioMappingMissingEvidenceFile planScenarioMappingState = "scenario mapping is missing an evidence file"
	planScenarioMappingMissingEvidenceText planScenarioMappingState = "scenario mapping is missing evidence text"
)

func canonicalPlanScenarioMappingStateFor(mapping canonicalPlanScenarioCoverage) planScenarioMappingState {
	return planScenarioMappingStateFor(mapping.title, mapping.evidence.file, mapping.evidence.text)
}

func markdownLinkRootPrefixScenarioMappingStateFor(mapping markdownLinkRootPrefixScenarioCoverage) planScenarioMappingState {
	return planScenarioMappingStateFor(mapping.title, mapping.evidence.file, mapping.evidence.text)
}

func planScenarioMappingStateFor(title string, evidenceFile string, evidenceText string) planScenarioMappingState {
	switch {
	case strings.TrimSpace(title) == "":
		return planScenarioMappingMissingTitle
	case strings.TrimSpace(evidenceFile) == "":
		return planScenarioMappingMissingEvidenceFile
	case strings.TrimSpace(evidenceText) == "":
		return planScenarioMappingMissingEvidenceText
	default:
		return planScenarioMappingComplete
	}
}

type planEvidenceReference struct {
	title string
	file  string
	text  string
}

type planEvidenceFileMatcher struct {
	repoRoot string
}

func matchPlanEvidenceFile(repoRoot string) types.GomegaMatcher {
	return planEvidenceFileMatcher{repoRoot: repoRoot}
}

func (matcher planEvidenceFileMatcher) Match(actual any) (bool, error) {
	reference, ok := actual.(planEvidenceReference)
	if !ok {
		return false, fmt.Errorf("plan evidence file matcher expects planEvidenceReference, got %T", actual)
	}
	if strings.TrimSpace(reference.file) == "" || strings.TrimSpace(reference.text) == "" {
		return false, nil
	}
	raw, err := os.ReadFile(filepath.Join(matcher.repoRoot, reference.file))
	if err != nil {
		return false, err
	}
	return ContainSubstring(reference.text).Match(string(raw))
}

func (matcher planEvidenceFileMatcher) FailureMessage(actual any) string {
	reference, ok := actual.(planEvidenceReference)
	if !ok {
		return fmt.Sprintf("Expected a plan evidence reference, got %T", actual)
	}
	return fmt.Sprintf("Expected plan scenario %q evidence file %q to contain mapped evidence %q", reference.title, reference.file, reference.text)
}

func (matcher planEvidenceFileMatcher) NegatedFailureMessage(actual any) string {
	reference, ok := actual.(planEvidenceReference)
	if !ok {
		return fmt.Sprintf("Expected a plan evidence reference not to match, got %T", actual)
	}
	return fmt.Sprintf("Expected plan scenario %q evidence file %q not to contain mapped evidence %q", reference.title, reference.file, reference.text)
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

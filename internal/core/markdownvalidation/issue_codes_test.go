package markdownvalidation

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

type workspaceStatusIssueContract struct {
	Code       IssueCode
	Severity   IssueSeverity
	MessageID  sharederrors.MessageID
	SourcePath tree.MarkdownPath
}

func matchWorkspaceStatusIssue(expected workspaceStatusIssueContract) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Code":       Equal(expected.Code),
		"Severity":   Equal(expected.Severity),
		"MessageID":  Equal(expected.MessageID),
		"SourcePath": Equal(expected.SourcePath),
	})
}

var _ = ginkgo.Describe("issue codes", ginkgo.Label("unit"), func() {
	ginkgo.It("uses typed fallback issue codes and error severity for workspace status", func() {
		result := ValidateWorkspaceStatus([]WorkspaceStatusIssue{{Path: "workspace", Message: "sync failed"}}, true)

		Expect(result.Issues).To(HaveExactElements(SatisfyAll(
			matchValidationIssueCode(IssueCodeWorkspaceSyncValidation),
			HaveField("Severity", IssueSeverityError),
		)))
	})

	ginkgo.It("maps typed issue codes and severities to stable contracts", func() {
		result := ValidateWorkspaceStatus([]WorkspaceStatusIssue{
			{Path: "duplicate.md", Code: IssueCodeDuplicateLeafwikiID, Message: "duplicate"},
			{Path: "warning.md", Severity: IssueSeverityWarning, Message: "warning"},
		}, true)

		Expect(result.Issues).To(ContainElements(
			SatisfyAll(
				matchValidationIssueCode(IssueCodeDuplicateLeafwikiID),
				HaveField("Severity", IssueSeverityError),
			),
			SatisfyAll(
				matchValidationIssueCode(IssueCodeWorkspaceSyncValidation),
				HaveField("Severity", IssueSeverityWarning),
			),
		))
	})

	ginkgo.It("normalizes workspace status issues into typed validation contracts", func() {
		customMessageID := newFixtureMessageID("validation.workspace.custom")

		result := ValidateWorkspaceStatus([]WorkspaceStatusIssue{
			{
				Path:      "workspace/link.md",
				Code:      IssueCodeBrokenLink,
				MessageID: customMessageID,
				Severity:  IssueSeverityWarning,
				Message:   "link failed",
			},
			{
				Path:    "workspace/fallback.md",
				Code:    newFixtureIssueCode(""),
				Message: "sync failed",
			},
		}, true)

		Expect(result.Issues).To(ConsistOf(
			matchWorkspaceStatusIssue(workspaceStatusIssueContract{
				Code:       IssueCodeBrokenLink,
				Severity:   IssueSeverityWarning,
				MessageID:  customMessageID,
				SourcePath: newFixtureMarkdownPath("workspace/link.md"),
			}),
			matchWorkspaceStatusIssue(workspaceStatusIssueContract{
				Code:       IssueCodeWorkspaceSyncValidation,
				Severity:   IssueSeverityError,
				MessageID:  MessageIDWorkspaceSyncValidation,
				SourcePath: newFixtureMarkdownPath("workspace/fallback.md"),
			}),
		))
	})
})

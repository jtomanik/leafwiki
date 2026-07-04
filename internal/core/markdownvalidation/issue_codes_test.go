package markdownvalidation

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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
})

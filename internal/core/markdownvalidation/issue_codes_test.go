package markdownvalidation

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("issue codes", func() {
	ginkgo.It("uses typed fallback issue codes and error severity for workspace status", func() {
		result := ValidateWorkspaceStatus([]WorkspaceStatusIssue{{Path: "workspace", Message: "sync failed"}}, true)

		Expect(result.Issues).To(HaveExactElements(SatisfyAll(
			matchValidationIssueCode(IssueCodeWorkspaceSyncValidation),
			HaveField("Severity", IssueSeverityError),
		)))
	})

	ginkgo.It("maps typed issue codes and severities to stable contracts", func() {
		Expect(IssueCodeDuplicateLeafwikiID.MessageID()).To(Equal(MessageIDDuplicateLeafwikiID))
		Expect(IssueSeverityWarning.Normalize(IssueSeverityError)).To(Equal(IssueSeverityWarning))
	})
})

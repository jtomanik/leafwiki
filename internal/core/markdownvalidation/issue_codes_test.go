package markdownvalidation

import (
	ginkgo "github.com/onsi/ginkgo/v2"
)

var _ = ginkgo.Describe("issue codes", func() {
	ginkgo.It("TestValidateWorkspaceStatusUsesTypedFallbackIssueCodeAndSeverity", func() {
		t := ginkgo.GinkgoT()
		t.Parallel()

		result := ValidateWorkspaceStatus([]WorkspaceStatusIssue{{Path: "workspace", Message: "sync failed"}}, true)

		if len(result.Issues) != 1 {
			t.Fatalf("issues = %#v, want one fallback issue", result.Issues)
		}
		issue := result.Issues[0]
		if issue.Code != IssueCodeWorkspaceSyncValidation {
			t.Fatalf("Code = %q, want %q", issue.Code, IssueCodeWorkspaceSyncValidation)
		}
		if issue.Severity != IssueSeverityError {
			t.Fatalf("Severity = %q, want %q", issue.Severity, IssueSeverityError)
		}
	})

	ginkgo.It("TestValidationIssueConstantsAreStable", func() {
		t := ginkgo.GinkgoT()
		t.Parallel()

		if IssueCodeDuplicateLeafwikiID.String() != "duplicate_leafwiki_id" {
			t.Fatalf("duplicate ID code = %q", IssueCodeDuplicateLeafwikiID)
		}
		if IssueSeverityWarning.String() != "warning" {
			t.Fatalf("warning severity = %q", IssueSeverityWarning)
		}
	})
})

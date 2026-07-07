package mcp

import (
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/localization"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

type catalogRenderState string
type messageTextState string

const (
	catalogRenderBacked  catalogRenderState = "catalog-backed"
	catalogRenderMissing catalogRenderState = "missing"
	catalogRenderErrored catalogRenderState = "errored"

	messageTextPresent messageTextState = "present"
	messageTextMissing messageTextState = "missing"
)

func matchCatalogMessageRender() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(catalogRenderStateFor, Equal(catalogRenderBacked))
}

func catalogRenderStateFor(result localization.Result) catalogRenderState {
	if result.Err != nil {
		return catalogRenderErrored
	}
	if result.Missing {
		return catalogRenderMissing
	}
	return catalogRenderBacked
}

type messageOutputProjection struct {
	MessageID ToolMessageID
	Message   messageTextState
}

func matchMessageOutput(messageID ToolMessageID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(messageOutputProjectionFor, Equal(messageOutputProjection{
		MessageID: messageID,
		Message:   messageTextPresent,
	}))
}

func messageOutputProjectionFor(output messageOutput) messageOutputProjection {
	messageState := messageTextMissing
	if output.Message != "" {
		messageState = messageTextPresent
	}
	return messageOutputProjection{
		MessageID: output.MessageID,
		Message:   messageState,
	}
}

type markdownValidationState string
type markdownValidationIssueState string

const (
	markdownValidationClean  markdownValidationState = "clean"
	markdownValidationFailed markdownValidationState = "failed"

	markdownValidationIssuePresent markdownValidationIssueState = "issue-present"
	markdownValidationIssueAbsent  markdownValidationIssueState = "issue-absent"
)

type markdownValidationProjection struct {
	State      markdownValidationState
	IssueCount int
}

type markdownValidationIssueProjection struct {
	State      markdownValidationState
	IssueCode  wikivalidation.IssueCode
	IssueState markdownValidationIssueState
}

func matchSuccessfulMarkdownValidation() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(result wikivalidation.Result) markdownValidationProjection {
		return markdownValidationProjectionFor(result)
	}, Equal(markdownValidationProjection{
		State:      markdownValidationClean,
		IssueCount: 0,
	}))
}

func matchMarkdownValidationWithoutIssue(code wikivalidation.IssueCode) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(result wikivalidation.Result) markdownValidationIssueProjection {
		return markdownValidationIssueProjectionFor(result, code)
	}, Equal(markdownValidationIssueProjection{
		State:      markdownValidationClean,
		IssueCode:  code,
		IssueState: markdownValidationIssueAbsent,
	}))
}

func matchMarkdownValidationWithIssue(code wikivalidation.IssueCode) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(result wikivalidation.Result) markdownValidationIssueProjection {
		return markdownValidationIssueProjectionFor(result, code)
	}, Equal(markdownValidationIssueProjection{
		State:      markdownValidationFailed,
		IssueCode:  code,
		IssueState: markdownValidationIssuePresent,
	}))
}

func markdownValidationProjectionFor(result wikivalidation.Result) markdownValidationProjection {
	state := markdownValidationFailed
	if result.OK {
		state = markdownValidationClean
	}
	return markdownValidationProjection{
		State:      state,
		IssueCount: len(result.Issues),
	}
}

func markdownValidationIssueProjectionFor(result wikivalidation.Result, code wikivalidation.IssueCode) markdownValidationIssueProjection {
	state := markdownValidationFailed
	if result.OK {
		state = markdownValidationClean
	}
	issueState := markdownValidationIssueAbsent
	for _, issue := range result.Issues {
		if issue.Code == code {
			issueState = markdownValidationIssuePresent
			break
		}
	}
	return markdownValidationIssueProjection{
		State:      state,
		IssueCode:  code,
		IssueState: issueState,
	}
}

type validationOutputState string

const (
	validationOutputErrors       validationOutputState = "errors"
	validationOutputWarningsOnly validationOutputState = "warnings-only"
	validationOutputClean        validationOutputState = "clean"
)

type validationOutputProjection struct {
	State    validationOutputState
	Errors   int
	Warnings int
}

func matchValidationOutputWithErrorCount(count int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(validationOutputProjectionFor, Equal(validationOutputProjection{
		State:  validationOutputErrors,
		Errors: count,
	}))
}

func matchValidationOutputWithWarningCount(count int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(validationOutputProjectionFor, Equal(validationOutputProjection{
		State:    validationOutputWarningsOnly,
		Warnings: count,
	}))
}

func validationOutputProjectionFor(output validationOutput) validationOutputProjection {
	state := validationOutputClean
	if output.Summary.Errors > 0 || !output.OK {
		state = validationOutputErrors
	} else if output.Summary.WarningCount > 0 {
		state = validationOutputWarningsOnly
	}
	return validationOutputProjection{
		State:    state,
		Errors:   output.Summary.Errors,
		Warnings: output.Summary.WarningCount,
	}
}

func matchMCPSyncLastErrorDetail(code sharederrors.ErrorCode) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(syncStatus map[string]any) any {
		return syncStatus["lastErrorDetail"]
	}, testmatchers.HaveStructuredError(code, sharederrors.MessageIDForCode(code)))
}

func matchMCPSyncLastErrorAbsent() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(syncStatus map[string]any) any {
		return syncStatus["lastErrorDetail"]
	}, BeNil())
}

func matchRedactedMCPSyncLastErrorDetail(code sharederrors.ErrorCode, forbidden ...string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(syncStatus map[string]any) any {
		return syncStatus["lastErrorDetail"]
	}, SatisfyAll(
		testmatchers.HaveStructuredError(code, sharederrors.MessageIDForCode(code)),
		HaveField("Message", rejectSubstrings(forbidden...)),
		HaveField("Template", rejectSubstrings(forbidden...)),
	))
}

func matchMCPToolErrorMeta(code sharederrors.ErrorCode) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(meta map[string]any) any {
		return meta["error"]
	}, SatisfyAll(
		testmatchers.HaveMCPStructuredError(code, sharederrors.MessageIDForCode(code)),
		HaveKeyWithValue("args", HaveLen(1)),
	))
}

func matchMCPToolErrorDetail(code sharederrors.ErrorCode) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(meta map[string]any) any {
		return meta["error"]
	}, testmatchers.HaveMCPStructuredError(code, sharederrors.MessageIDForCode(code)))
}

type validationIssueOutputProjection struct {
	Code      wikivalidation.IssueCode
	Path      string
	MessageID sharederrors.MessageID
	Severity  wikivalidation.IssueSeverity
}

func matchValidationIssueOutput(code wikivalidation.IssueCode, path string, messageID sharederrors.MessageID, severity wikivalidation.IssueSeverity) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(validationIssueOutputProjectionFor, Equal(validationIssueOutputProjection{
		Code:      code,
		Path:      path,
		MessageID: messageID,
		Severity:  severity,
	}))
}

func validationIssueOutputProjectionFor(issue any) validationIssueOutputProjection {
	switch issue := issue.(type) {
	case validationIssueOutput:
		return validationIssueOutputProjection{
			Code:      issue.Code,
			Path:      issue.Path,
			MessageID: issue.MessageID,
			Severity:  issue.Severity,
		}
	case wikivalidation.WorkspaceStatusIssue:
		return validationIssueOutputProjection{
			Code:      issue.Code,
			Path:      issue.Path,
			MessageID: issue.MessageID,
			Severity:  issue.Severity,
		}
	default:
		return validationIssueOutputProjection{}
	}
}

func matchRedactedValidationIssueOutput(path string, requiredMessage string, forbidden ...string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return SatisfyAll(
		HaveField("Path", Equal(path)),
		HaveField("Message", SatisfyAll(ContainSubstring(requiredMessage), rejectSubstrings(forbidden...))),
	)
}

func matchWorkspaceValidationError(path string, requiredMessage string, forbidden ...string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return SatisfyAll(
		HaveField("Path", Equal(path)),
		HaveField("Message", SatisfyAll(ContainSubstring(requiredMessage), rejectSubstrings(forbidden...))),
	)
}

func matchMCPRefreshValidation(errorCount int, issueMatcher types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return SatisfyAll(
		HaveField("Summary.Errors", Equal(errorCount)),
		HaveField("Issues", HaveExactElements(issueMatcher)),
	)
}

func rejectSubstrings(values ...string) types.GomegaMatcher {
	if len(values) == 0 {
		return BeAssignableToTypeOf("")
	}
	matchers := make([]types.GomegaMatcher, 0, len(values))
	for _, value := range values {
		matchers = append(matchers, Not(ContainSubstring(value)))
	}
	return SatisfyAll(matchers...)
}

func matchWorkspaceValidationErrorDetails(issueMatcher types.GomegaMatcher) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(func(syncStatus map[string]any) any {
		return syncStatus["validationErrorDetails"]
	}, HaveExactElements(issueMatcher))
}

type searchPaginationState string

type searchPagesResultLimit struct {
	Value int
}

const (
	searchPaginationHasMore  searchPaginationState = "has-more"
	searchPaginationComplete searchPaginationState = "complete"
)

type searchPagesProjection struct {
	Limit      searchPagesResultLimit
	Pagination searchPaginationState
}

func matchSearchPagesOutputWithMoreResults(limit int) types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(searchPagesProjectionFor, Equal(searchPagesProjection{
		Limit:      searchPagesResultLimitFromInt(limit),
		Pagination: searchPaginationHasMore,
	}))
}

func searchPagesProjectionFor(output searchPagesOutput) searchPagesProjection {
	pagination := searchPaginationComplete
	if output.HasMore {
		pagination = searchPaginationHasMore
	}
	return searchPagesProjection{
		Limit:      searchPagesResultLimitFromInt(output.Limit),
		Pagination: pagination,
	}
}

func searchPagesResultLimitFromInt(limit int) searchPagesResultLimit {
	return searchPagesResultLimit{Value: limit}
}

type updatePagePatchFieldState string

const (
	updatePagePatchFieldAbsent        updatePagePatchFieldState = "absent"
	updatePagePatchFieldExplicitEmpty updatePagePatchFieldState = "explicit-empty"
	updatePagePatchFieldPopulated     updatePagePatchFieldState = "populated"
)

type updatePagePatchProjection struct {
	Tags       updatePagePatchFieldState
	Properties updatePagePatchFieldState
}

func matchUpdatePageInputWithExplicitEmptyPatchFields() types.GomegaMatcher {
	ginkgo.GinkgoHelper()
	return WithTransform(updatePagePatchProjectionFor, Equal(updatePagePatchProjection{
		Tags:       updatePagePatchFieldExplicitEmpty,
		Properties: updatePagePatchFieldExplicitEmpty,
	}))
}

func updatePagePatchProjectionFor(input updatePageInput) updatePagePatchProjection {
	return updatePagePatchProjection{
		Tags:       updatePageTagsPatchFieldStateFor(input),
		Properties: updatePagePropertiesPatchFieldStateFor(input),
	}
}

func updatePageTagsPatchFieldStateFor(input updatePageInput) updatePagePatchFieldState {
	if !input.TagsPresent {
		return updatePagePatchFieldAbsent
	}
	if len(input.Tags) == 0 {
		return updatePagePatchFieldExplicitEmpty
	}
	return updatePagePatchFieldPopulated
}

func updatePagePropertiesPatchFieldStateFor(input updatePageInput) updatePagePatchFieldState {
	if !input.PropertiesPresent {
		return updatePagePatchFieldAbsent
	}
	if len(input.Properties) == 0 {
		return updatePagePatchFieldExplicitEmpty
	}
	return updatePagePatchFieldPopulated
}

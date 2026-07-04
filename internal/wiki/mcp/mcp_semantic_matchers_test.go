package mcp

import (
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	wikivalidation "github.com/perber/wiki/internal/core/markdownvalidation"
	"github.com/perber/wiki/internal/localization"
)

type catalogRenderState string

const (
	catalogRenderBacked  catalogRenderState = "catalog-backed"
	catalogRenderMissing catalogRenderState = "missing"
	catalogRenderErrored catalogRenderState = "errored"
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

package importer

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/perber/wiki/internal/core/assets"
	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"
)

type ExecutionResult struct {
	ImportedCount  int                   `json:"imported_count"`
	UpdatedCount   int                   `json:"updated_count"`
	SkippedCount   int                   `json:"skipped_count"`
	Items          []ExecutionItemResult `json:"items"`
	TreeHash       string                `json:"tree_hash"`        // hash of the state of the wiki tree after import
	TreeHashBefore string                `json:"tree_hash_before"` // hash of the state of the wiki tree before import
}

type ExecutionProgress struct {
	ProcessedItems        int        `json:"processed_items"`
	TotalItems            int        `json:"total_items"`
	CurrentItemSourcePath *string    `json:"current_item_source_path,omitempty"`
	StartedAt             *time.Time `json:"started_at,omitempty"`
	FinishedAt            *time.Time `json:"finished_at,omitempty"`
}

type ExecutionAction string

const (
	ExecutionActionCreated ExecutionAction = "created"
	ExecutionActionUpdated ExecutionAction = "updated"
	ExecutionActionSkipped ExecutionAction = "skipped"
)

type ExecutionItemResult struct {
	SourcePath tree.WorkspaceSourcePath `json:"source_path"`
	TargetPath tree.RoutePath           `json:"target_path"`
	Action     ExecutionAction          `json:"action"`
	Error      *string                  `json:"error,omitempty"`
	ErrorCode  ImportErrorCode          `json:"error_code,omitempty"`
	Notes      []string                 `json:"notes,omitempty"`
}

const (
	importerLogFieldTargetPath = "target_path"
	importerLogFieldSourcePath = "source_path"
	importerLogFieldPageID     = "page_id"
)

func (item *ExecutionItemResult) fail(code ImportErrorCode, message string) {
	item.Action = ExecutionActionSkipped
	item.Error = &message
	item.ErrorCode = code
}

func (item *ExecutionItemResult) failWithError(code ImportErrorCode, err error) {
	item.fail(code, err.Error())
}

type Executor struct {
	plan                   *PlanResult
	planOptions            *PlanOptions
	assetMaxBytes          shared.MaxBytes
	wiki                   ImporterWiki
	logger                 *slog.Logger
	markdownLinkRootPrefix string
	progressFn             func(ExecutionProgress, *ExecutionResult)
	cancelFn               func() bool
	startIndex             int
	initialResult          *ExecutionResult
}

type ExecutorOptions struct {
	MarkdownLinkRootPrefix string
}

func NewExecutor(plan *PlanResult, planOptions *PlanOptions, assetMaxBytes shared.MaxBytes, wiki ImporterWiki, logger *slog.Logger) *Executor {
	return NewExecutorWithOptions(plan, planOptions, assetMaxBytes, wiki, logger, ExecutorOptions{})
}

func NewExecutorWithOptions(plan *PlanResult, planOptions *PlanOptions, assetMaxBytes shared.MaxBytes, wiki ImporterWiki, logger *slog.Logger, opts ExecutorOptions) *Executor {
	if assetMaxBytes <= 0 {
		assetMaxBytes = assets.DefaultMaxUploadSizeBytes
	}
	return &Executor{
		plan:                   plan,
		planOptions:            planOptions,
		assetMaxBytes:          assetMaxBytes,
		wiki:                   wiki,
		logger:                 logger.With("component", "ImporterExecutor"),
		markdownLinkRootPrefix: opts.MarkdownLinkRootPrefix,
	}
}

func (e *Executor) WithProgressCallback(progressFn func(ExecutionProgress, *ExecutionResult)) *Executor {
	e.progressFn = progressFn
	return e
}

func (e *Executor) WithCancelCheck(cancelFn func() bool) *Executor {
	e.cancelFn = cancelFn
	return e
}

func (e *Executor) WithResumeState(startIndex int, initialResult *ExecutionResult) *Executor {
	e.startIndex = startIndex
	e.initialResult = cloneExecutionResult(initialResult)
	return e
}

func buildImportedContent(mdFile *markdown.MarkdownFile, page *tree.Page, body string) (string, error) {
	meta := mdFile.GetMetadata()
	if meta.Version == 0 {
		meta.Version = 1
	}
	meta.Page = markdown.PageMetadataPage{
		ID:    page.ID.MetadataValue(),
		Title: strings.TrimSpace(page.Title),
	}
	return markdown.RenderPageDocument(markdown.PageDocument{
		Body:     body,
		Metadata: meta,
	})
}

// Execute runs the import based on the provided plan
func (e *Executor) Execute(userID tree.UserID) (*ExecutionResult, error) {
	beforeExecution := e.wiki.TreeHash()
	expectedTreeHash := e.plan.TreeHash
	if e.startIndex > 0 {
		if e.initialResult == nil || e.initialResult.TreeHash == "" {
			return nil, ErrImportResumeTreeHashMissing
		}
		expectedTreeHash = e.initialResult.TreeHash
	}
	if expectedTreeHash != beforeExecution {
		return nil, fmt.Errorf("plan is stale: %w: expected tree_hash %s but got %s", ErrImportPlanStale, expectedTreeHash, beforeExecution)
	}

	transformer := newContentTransformerWithOptions(e.plan, e.planOptions.SourceBasePath, e.assetMaxBytes, ContentTransformerOptions{
		MarkdownLinkRootPrefix: e.markdownLinkRootPrefix,
	})
	startedAt := time.Now()

	result := cloneExecutionResult(e.initialResult)
	if result == nil {
		result = &ExecutionResult{
			TreeHashBefore: beforeExecution,
		}
	}
	if result.TreeHashBefore == "" {
		result.TreeHashBefore = beforeExecution
	}

	e.reportProgress(ExecutionProgress{
		ProcessedItems: e.startIndex,
		TotalItems:     len(e.plan.Items),
		StartedAt:      &startedAt,
	}, result)

	for index := e.startIndex; index < len(e.plan.Items); index++ {
		if e.cancelFn != nil && e.cancelFn() {
			return result, ErrImportCanceled
		}

		item := e.plan.Items[index]
		currentItemSourcePath := item.SourcePath.FilesystemPath()
		e.reportProgress(ExecutionProgress{
			ProcessedItems:        index,
			TotalItems:            len(e.plan.Items),
			CurrentItemSourcePath: &currentItemSourcePath,
			StartedAt:             &startedAt,
		}, result)

		execItem := ExecutionItemResult{
			SourcePath: item.SourcePath,
			TargetPath: item.TargetPath,
			Notes:      append([]string{}, item.Notes...),
			Error:      nil,
		}

		switch item.Action {
		case PlanActionCreate:
			// Creates the page or section and also all necessary parent sections
			page, err := e.wiki.EnsurePath(userID, item.TargetPath, item.Title, &item.Kind)
			if err != nil {
				execItem.failWithError(ImportErrorCodeEnsurePathFailed, err)
				result.SkippedCount++
				result.Items = append(result.Items, execItem)
				e.logger.Error("Failed to ensure path", importerLogFieldTargetPath, item.TargetPath, "error", err)
				continue
			}
			// Read the content from the source path
			// And update the page content
			if page == nil {
				errMsg := "could not create page"
				execItem.fail(ImportErrorCodeCreatePageFailed, errMsg)
				result.SkippedCount++
				result.Items = append(result.Items, execItem)
				e.logger.Error("Could not create page", importerLogFieldTargetPath, item.TargetPath, "error", errMsg)
				continue
			}
			sourceAbs := filepath.Join(e.planOptions.SourceBasePath, filepath.FromSlash(item.SourcePath.FilesystemPath()))
			mdFile, err := markdown.LoadMarkdownFile(sourceAbs)
			if err != nil {
				execItem.failWithError(ImportErrorCodeLoadSourceFailed, err)
				result.SkippedCount++
				result.Items = append(result.Items, execItem)
				e.logger.Error("Failed to load source file", importerLogFieldSourcePath, sourceAbs, "error", err)
				continue
			}
			importedBody, err := transformer.TransformContent(userID, item.SourcePath, page, mdFile.GetContent(), e.wiki)
			if err != nil {
				execItem.failWithError(ImportErrorCodeTransformContentFailed, err)
				result.SkippedCount++
				result.Items = append(result.Items, execItem)
				e.logger.Error("Failed to transform imported content", importerLogFieldSourcePath, sourceAbs, "error", err)
				continue
			}
			importedContent, err := buildImportedContent(mdFile, page, importedBody)
			if err != nil {
				execItem.failWithError(ImportErrorCodeRenderImportedContent, err)
				result.SkippedCount++
				result.Items = append(result.Items, execItem)
				e.logger.Error("Failed to prepare imported content", importerLogFieldSourcePath, sourceAbs, "error", err)
				continue
			}
			if _, err := e.wiki.UpdatePage(userID, page.ID, page.Title, page.Slug, &importedContent, &page.Kind); err != nil {
				execItem.failWithError(ImportErrorCodeUpdatePageFailed, err)
				result.SkippedCount++
				result.Items = append(result.Items, execItem)
				e.logger.Error("Failed to update page content", importerLogFieldPageID, page.ID, "error", err)
				continue
			}
			execItem.Action = ExecutionActionCreated
			result.ImportedCount++
			e.logger.Info("Imported page", importerLogFieldSourcePath, item.SourcePath, importerLogFieldTargetPath, item.TargetPath, importerLogFieldPageID, page.ID)
		case PlanActionSkip:
			execItem.Action = ExecutionActionSkipped
			e.logger.Info("Skipped page", importerLogFieldSourcePath, item.SourcePath, importerLogFieldTargetPath, item.TargetPath)
			result.SkippedCount++
		default:
			errMsg := "unknown action"
			execItem.fail(ImportErrorCodeUnknownAction, errMsg)
			e.logger.Info("Skipped page with unknown action", importerLogFieldSourcePath, item.SourcePath, importerLogFieldTargetPath, item.TargetPath)
			result.SkippedCount++
		}

		result.Items = append(result.Items, execItem)
		result.TreeHash = e.wiki.TreeHash()
		e.reportProgress(ExecutionProgress{
			ProcessedItems: index + 1,
			TotalItems:     len(e.plan.Items),
			StartedAt:      &startedAt,
		}, result)
	}

	result.TreeHash = e.wiki.TreeHash()
	finishedAt := time.Now()
	e.reportProgress(ExecutionProgress{
		ProcessedItems: len(e.plan.Items),
		TotalItems:     len(e.plan.Items),
		StartedAt:      &startedAt,
		FinishedAt:     &finishedAt,
	}, result)

	return result, nil
}

func (e *Executor) reportProgress(progress ExecutionProgress, result *ExecutionResult) {
	if e.progressFn == nil {
		return
	}
	e.progressFn(progress, result)
}

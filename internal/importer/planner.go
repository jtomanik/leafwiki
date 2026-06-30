package importer

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/shared"
	"github.com/perber/wiki/internal/core/tree"
)

type PlanAction string

const (
	PlanActionCreate PlanAction = "create" // creates new node
	PlanActionUpdate PlanAction = "update" // updates existing node
	PlanActionSkip   PlanAction = "skip"   // skips existing node
)

type ImportErrorCode string

const (
	ImportErrorCodeEnsurePathFailed          ImportErrorCode = "ensure_path_failed"
	ImportErrorCodeCreatePageFailed          ImportErrorCode = "create_page_failed"
	ImportErrorCodeLoadSourceFailed          ImportErrorCode = "load_source_failed"
	ImportErrorCodeTransformContentFailed    ImportErrorCode = "transform_content_failed"
	ImportErrorCodeRenderImportedContent     ImportErrorCode = "render_imported_content_failed"
	ImportErrorCodeUpdatePageFailed          ImportErrorCode = "update_page_failed"
	ImportErrorCodeUnknownAction             ImportErrorCode = "unknown_action"
	ImportErrorCodeSourceStatFailed          ImportErrorCode = "source_stat_failed"
	ImportErrorCodeSourceIsDirectory         ImportErrorCode = "source_is_directory"
	ImportErrorCodeNormalizeSourcePathFailed ImportErrorCode = "normalize_source_path_failed"
	ImportErrorCodeNormalizeFilenameFailed   ImportErrorCode = "normalize_filename_failed"
	ImportErrorCodeLookupPathFailed          ImportErrorCode = "lookup_path_failed"
	ImportErrorCodeInvalidLookupResult       ImportErrorCode = "invalid_lookup_result"
)

type PlanError struct {
	SourcePath tree.WorkspaceSourcePath `json:"source_path,omitempty"`
	Code       ImportErrorCode          `json:"code"`
	Error      string                   `json:"error"`
}

type importPlanError struct {
	code ImportErrorCode
	err  error
}

func (err importPlanError) Error() string {
	if err.err == nil {
		return ""
	}
	return err.err.Error()
}

func (err importPlanError) Unwrap() error {
	return err.err
}

func newImportPlanError(code ImportErrorCode, err error) error {
	if err == nil {
		return nil
	}
	return importPlanError{code: code, err: err}
}

func importPlanErrorCode(err error) ImportErrorCode {
	var planErr importPlanError
	if errors.As(err, &planErr) {
		return planErr.code
	}
	return ""
}

// ImportMDFile represents a markdown file to be imported
type ImportMDFile struct {
	SourcePath tree.WorkspaceSourcePath // relative path to the markdown file in the zip directory
}

// PlanItem represents a single item in the import plan
type PlanItem struct {
	SourcePath  tree.WorkspaceSourcePath `json:"source_path"`
	TargetPath  tree.RoutePath           `json:"target_path"`
	Title       string                   `json:"title"`
	DesiredSlug tree.Slug                `json:"desired_slug"`
	Kind        tree.NodeKind            `json:"kind"`
	Exists      bool                     `json:"exists"`
	ExistingID  *tree.PageID             `json:"existing_id"`

	Action    PlanAction `json:"action"`
	Conflicts []string   `json:"conflicts"`
	Notes     []string   `json:"notes"`
}

// PlanOptions represents options for creating an import plan
type PlanOptions struct {
	SourceBasePath string // base path in the import source
	TargetBasePath string // base path in the wiki where to import
}

// PlanResult represents the result of the import plan
type PlanResult struct {
	ID           string      `json:"id"`
	TreeHash     string      `json:"tree_hash"` // hash of the state of the wiki tree before import
	Items        []PlanItem  `json:"items"`
	Errors       []string    `json:"errors"`
	ErrorDetails []PlanError `json:"error_details,omitempty"`
}

// Planner is responsible for creating an import plan
type Planner struct {
	log     *slog.Logger
	wiki    ImporterWiki
	slugger *tree.SlugService
}

var importerGenerateUniqueID = shared.GenerateUniqueID

// NewPlanner creates a new Planner
func NewPlanner(wiki ImporterWiki, slugger *tree.SlugService) *Planner {
	return &Planner{
		log:     slog.Default().With("component", "Planner"),
		wiki:    wiki,
		slugger: slugger,
	}
}

// CreatePlan creates an import plan based on the provided entries and options
func (p *Planner) CreatePlan(entries []ImportMDFile, options PlanOptions) (*PlanResult, error) {
	// Generate a unique ID for the new page
	id, err := importerGenerateUniqueID()
	if err != nil {
		return nil, fmt.Errorf("could not generate unique ID: %w", err)
	}
	result := &PlanResult{
		ID:           id,
		Items:        []PlanItem{},
		Errors:       []string{},
		ErrorDetails: []PlanError{},
		TreeHash:     p.wiki.TreeHash(),
	}
	for _, entry := range entries {
		resEntry, err := p.analyzeEntry(entry, options)
		if err != nil {
			p.log.Warn("could not import resource", "source_path", entry.SourcePath, "error", err)
			errMsg := err.Error()
			result.Errors = append(result.Errors, errMsg)
			result.ErrorDetails = append(result.ErrorDetails, PlanError{
				SourcePath: entry.SourcePath,
				Code:       importPlanErrorCode(err),
				Error:      errMsg,
			})
			continue
		}

		result.Items = append(result.Items, *resEntry)
	}
	return result, nil
}

// analyzeEntry analyzes a entry (directory or file) to be imported
func (p *Planner) analyzeEntry(mdFile ImportMDFile, options PlanOptions) (*PlanItem, error) {
	// FS path for reading
	sourcePath := filepath.Join(options.SourceBasePath, filepath.FromSlash(mdFile.SourcePath.FilesystemPath()))

	// Validate if sourcePath exists and is a file
	info, err := os.Stat(sourcePath)
	if err != nil {
		return nil, newImportPlanError(ImportErrorCodeSourceStatFailed, err)
	}
	if info.IsDir() {
		return nil, newImportPlanError(ImportErrorCodeSourceIsDirectory, errors.New("source path is a directory, expected a file: "+mdFile.SourcePath.FilesystemPath()))
	}

	// normalize source path (zip-ish)
	rel := filepath.ToSlash(strings.TrimSpace(mdFile.SourcePath.FilesystemPath()))
	rel = strings.TrimPrefix(rel, "/")

	sourceFilename := path.Base(rel)
	filenameLower := strings.ToLower(sourceFilename)
	sourceDir := path.Dir(rel)
	if sourceDir == "." {
		sourceDir = ""
	}

	// Normalize import path segments into valid route slugs before we look anything up in the wiki.
	// At planning time we intentionally do not try to enforce sibling uniqueness in the tree.
	normalizedSourceDir, err := p.slugger.NormalizePathToValidSlugs(sourceDir)
	if err != nil {
		return nil, newImportPlanError(ImportErrorCodeNormalizeSourcePathFailed, err)
	}
	normalizedSourceDir = strings.Trim(normalizedSourceDir, "/")

	// compute wiki path (route)
	targetBase := strings.Trim(strings.TrimSpace(options.TargetBasePath), "/")

	kind := tree.NodeKindPage
	var wikiPath tree.RoutePath

	readmeFallback := sourceFilename == "README.md" && !p.sourceDirHasIndex(options.SourceBasePath, sourceDir)
	if filenameLower == "index.md" || readmeFallback {
		kind = tree.NodeKindSection
		wikiPath = tree.RoutePathFromString(strings.Trim(path.Join(targetBase, normalizedSourceDir), "/"))
	} else {
		// File names map to page slugs, so we normalize the basename but preserve the extension.
		normalizedFilename, err := p.slugger.NormalizeFilenameToValidSlug(filenameLower) // e.g. "my-page.md"
		if err != nil {
			return nil, newImportPlanError(ImportErrorCodeNormalizeFilenameFailed, err)
		}
		baseSlug := strings.TrimSuffix(normalizedFilename, path.Ext(normalizedFilename))
		if sourceFilename == "README.md" {
			baseSlug = "README"
		}
		wikiPath = tree.RoutePathFromString(strings.Trim(path.Join(targetBase, normalizedSourceDir, baseSlug), "/"))
	}

	// lookup existing
	result, err := p.wiki.LookupPagePathForKind(wikiPath, kind)
	if err != nil {
		return nil, newImportPlanError(ImportErrorCodeLookupPathFailed, err)
	}

	var notes []string
	md, err := markdown.LoadMarkdownFile(sourcePath)
	if err != nil {
		notes = append(notes, fmt.Sprintf("Failed to load markdown file for title extraction: %v", err))
	}

	// Determine fallback title
	title := wikiPath.LeafSlug().FilesystemPath() // fallback to last segment of wiki path
	if wikiPath == "" {
		// For root-level index.md or empty paths, use filename without extension
		title = strings.TrimSuffix(filenameLower, path.Ext(filenameLower))
	}

	if md != nil {
		title, _ = md.GetTitle()
	}

	if !result.Exists {
		// slug = last segment
		var slug tree.Slug
		if wikiPath != "" {
			slug = wikiPath.LeafSlug()
		}

		return &PlanItem{
			SourcePath:  mdFile.SourcePath,
			TargetPath:  wikiPath,
			Title:       title,
			DesiredSlug: slug,
			Kind:        kind,
			Exists:      false,
			Action:      PlanActionCreate,
			Notes:       notes,
		}, nil
	}

	if len(result.Segments) == 0 {
		return nil, newImportPlanError(ImportErrorCodeInvalidLookupResult, errors.New("invalid lookup result with zero segments for existing path"))
	}

	last := result.Segments[len(result.Segments)-1]
	var existingID *tree.PageID
	if last.ID != nil {
		existingID = last.ID
	}
	return &PlanItem{
		SourcePath:  mdFile.SourcePath,
		TargetPath:  wikiPath,
		Title:       title,
		DesiredSlug: last.Slug,
		Exists:      true,
		ExistingID:  existingID,
		Kind:        kind,
		Action:      PlanActionSkip,
		Notes:       notes,
	}, nil
}

func (p *Planner) sourceDirHasIndex(sourceBasePath string, sourceDir string) bool {
	_, ok := sourceDirIndexFile(sourceBasePath, sourceDir)
	return ok
}

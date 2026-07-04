package pages

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

func ValidatePageRoutePath(routePath string) (tree.RoutePath, error) {
	validPath, err := tree.ValidateRoutePath(routePath)
	if err == nil {
		return validPath, nil
	}
	trimmed := strings.TrimSpace(routePath)
	if trimmed == "" {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodePageMissingPath, nil)
	}
	return "", sharederrors.NewLocalizedErrorFromCode(ErrCodePageInvalidPath, nil, trimmed)
}

func ValidatePageKind(kind *string) (tree.NodeKind, error) {
	if kind == nil {
		return tree.NodeKindPage, nil
	}
	validKind, ok := tree.ParseNodeKind(*kind)
	if !ok {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodePageInvalidKind, nil)
	}
	return validKind, nil
}

func ValidatePageKindString(kind string) (tree.NodeKind, error) {
	return ValidatePageKind(&kind)
}

func NormalizePagePathInput(rawPath string, rawKind string) (tree.RoutePath, tree.NodeKind, error) {
	var kind tree.NodeKind
	if strings.TrimSpace(rawKind) != "" {
		validKind, err := ValidatePageKindString(strings.TrimSpace(rawKind))
		if err != nil {
			return "", "", err
		}
		kind = validKind
	}
	return NormalizePagePathKindInput(rawPath, kind)
}

func NormalizePagePathKindInput(rawPath string, kind tree.NodeKind) (tree.RoutePath, tree.NodeKind, error) {
	routePath := strings.Trim(strings.TrimSpace(rawPath), "/")
	if derivedKind := MarkdownPathInputKind(tree.MarkdownPathFromString(routePath)); derivedKind != "" {
		routePath = tree.MarkdownPathToRoutePath(routePath)
		if kind != "" && kind != derivedKind {
			return "", "", sharederrors.NewLocalizedErrorFromCode(ErrCodePageInvalidKind, nil)
		}
		kind = derivedKind
	}
	validPath, err := ValidatePageRoutePath(routePath)
	if err != nil {
		return "", "", err
	}
	return validPath, kind, nil
}

func MarkdownPathInputKind(markdownPath tree.MarkdownPath) tree.NodeKind {
	if !strings.EqualFold(markdownPath.Ext(), ".md") {
		return ""
	}
	if markdownPath.IsIndexFile() {
		return tree.NodeKindSection
	}
	return tree.NodeKindPage
}

func MarkdownContentPathForRoute(routePath tree.RoutePath, kind tree.NodeKind) tree.MarkdownPath {
	return routePath.MarkdownContentPath(kind)
}

func ReadmeMarkdownPathFallbackRoutes(rawPath string) (string, string, bool) {
	trimmed := strings.Trim(strings.TrimSpace(rawPath), "/")
	if path.Base(trimmed) != "README.md" {
		return "", "", false
	}
	pageRoute := tree.MarkdownPathToRoutePath(trimmed)
	sectionRoute := ""
	if trimmed != "README.md" {
		sectionRoute = strings.TrimSuffix(trimmed, "/README.md")
		sectionRoute = strings.Trim(sectionRoute, "/")
	}
	return pageRoute, sectionRoute, true
}

type ReadmeMarkdownPathFallbackInput struct {
	PageRoute    string
	SectionRoute string
	TryPage      bool
	TrySection   bool
}

type ReadmeMarkdownPathFallbackLookup struct {
	RootDir    string
	FindByPath func(FindByPathInput) (*FindByPathOutput, error)
	RootPage   func() (*tree.Page, error)
}

func NormalizeReadmeMarkdownPathFallbackInput(rawPath string, kind tree.NodeKind) (ReadmeMarkdownPathFallbackInput, bool, error) {
	pageRoute, sectionRoute, ok := ReadmeMarkdownPathFallbackRoutes(rawPath)
	if !ok {
		return ReadmeMarkdownPathFallbackInput{}, false, nil
	}
	return normalizeReadmeMarkdownPathFallbackRoutes(pageRoute, sectionRoute, kind)
}

func NormalizeReadmeMarkdownPathFallbackRawInput(rawPath string, rawKind string) (ReadmeMarkdownPathFallbackInput, bool, error) {
	pageRoute, sectionRoute, ok := ReadmeMarkdownPathFallbackRoutes(rawPath)
	if !ok {
		return ReadmeMarkdownPathFallbackInput{}, false, nil
	}
	var kind tree.NodeKind
	if strings.TrimSpace(rawKind) != "" {
		validKind, err := ValidatePageKindString(strings.TrimSpace(rawKind))
		if err != nil {
			return ReadmeMarkdownPathFallbackInput{}, true, err
		}
		kind = validKind
	}
	return normalizeReadmeMarkdownPathFallbackRoutes(pageRoute, sectionRoute, kind)
}

func normalizeReadmeMarkdownPathFallbackRoutes(pageRoute string, sectionRoute string, kind tree.NodeKind) (ReadmeMarkdownPathFallbackInput, bool, error) {
	input := ReadmeMarkdownPathFallbackInput{
		PageRoute:    pageRoute,
		SectionRoute: sectionRoute,
	}
	switch kind {
	case "":
		input.TryPage = true
		input.TrySection = true
	case tree.NodeKindPage:
		input.TryPage = true
	case tree.NodeKindSection:
		input.TrySection = true
	default:
		return ReadmeMarkdownPathFallbackInput{}, true, sharederrors.NewLocalizedErrorFromCode(ErrCodePageInvalidKind, nil)
	}
	return input, true, nil
}

func FindReadmeMarkdownPathFallback(rawPath string, kind tree.NodeKind, lookup ReadmeMarkdownPathFallbackLookup) (*FindByPathOutput, bool, error) {
	fallback, ok, err := NormalizeReadmeMarkdownPathFallbackInput(rawPath, kind)
	if err != nil || !ok {
		return nil, ok, err
	}
	return findReadmeMarkdownPathFallback(fallback, lookup)
}

func FindReadmeMarkdownPathFallbackRawInput(rawPath string, rawKind string, lookup ReadmeMarkdownPathFallbackLookup) (*FindByPathOutput, bool, error) {
	fallback, ok, err := NormalizeReadmeMarkdownPathFallbackRawInput(rawPath, rawKind)
	if err != nil || !ok {
		return nil, ok, err
	}
	return findReadmeMarkdownPathFallback(fallback, lookup)
}

func findReadmeMarkdownPathFallback(fallback ReadmeMarkdownPathFallbackInput, lookup ReadmeMarkdownPathFallbackLookup) (*FindByPathOutput, bool, error) {
	var pageErr error
	if fallback.TryPage {
		pageRoute, err := ValidatePageRoutePath(fallback.PageRoute)
		if err != nil {
			return nil, true, err
		}
		var pageOut *FindByPathOutput
		pageOut, pageErr = lookup.FindByPath(FindByPathInput{RoutePath: pageRoute, Kind: tree.NodeKindPage})
		if pageErr == nil {
			return pageOut, true, nil
		}
	}
	if fallback.TrySection && fallback.SectionRoute != "" {
		if _, err := ValidatePageRoutePath(fallback.SectionRoute); err != nil {
			return nil, true, err
		}
	}
	if !fallback.TrySection || !ReadmeFallbackSectionIsActive(lookup.RootDir, fallback.SectionRoute) {
		if pageErr != nil {
			return nil, true, pageErr
		}
		return nil, true, tree.ErrPageNotFound
	}
	if fallback.SectionRoute == "" {
		page, err := lookup.RootPage()
		if err != nil {
			return nil, true, err
		}
		return &FindByPathOutput{Page: page}, true, nil
	}
	sectionRoute, _ := ValidatePageRoutePath(fallback.SectionRoute)
	out, err := lookup.FindByPath(FindByPathInput{RoutePath: sectionRoute, Kind: tree.NodeKindSection})
	return out, true, err
}

func ReadmeFallbackSectionIsActive(rootDir string, sectionRoute string) bool {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return false
	}
	sectionRoute = strings.Trim(strings.TrimSpace(sectionRoute), "/")
	sectionDir := rootDir
	if sectionRoute != "" {
		sectionDir = filepath.Join(rootDir, filepath.FromSlash(sectionRoute))
	}
	entries, err := os.ReadDir(sectionDir)
	if err != nil {
		return false
	}
	hasReadme := false
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "README.md" {
			hasReadme = true
			continue
		}
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		if strings.EqualFold(base, "index") && strings.EqualFold(ext, ".md") {
			return false
		}
	}
	return hasReadme
}

func ValidateRefactorKind(kind string) (string, error) {
	switch kind {
	case RefactorKindRename, RefactorKindMove:
		return kind, nil
	default:
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodePageInvalidRefactorKind, nil)
	}
}

func ValidateMoveParentID(parentID string) (string, error) {
	if parentID == "" || parentID == "root" {
		return parentID, nil
	}
	if strings.TrimSpace(parentID) != parentID {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodePageInvalidParentID, nil)
	}
	return parentID, nil
}

func ValidateSemanticMoveParentID(parentID tree.PageID) (tree.PageID, error) {
	if parentID == "" || parentID == tree.RootPageID {
		return parentID, nil
	}
	raw := parentID.MetadataValue()
	if raw != parentID.HashPayload() {
		return "", sharederrors.NewLocalizedErrorFromCode(ErrCodePageInvalidParentID, nil)
	}
	return tree.PageIDFromString(raw), nil
}

func ValidateOptionalParentID(parentID *string) (*string, error) {
	if parentID == nil {
		var absentParentID *string
		return absentParentID, nil
	}
	validated, err := ValidateMoveParentID(*parentID)
	if err != nil {
		return nil, err
	}
	return &validated, nil
}

func ValidateOptionalSemanticParentID(parentID *tree.PageID) (*tree.PageID, error) {
	if parentID == nil {
		var absentParentID *tree.PageID
		return absentParentID, nil
	}
	validated, err := ValidateSemanticMoveParentID(*parentID)
	if err != nil {
		return nil, err
	}
	return &validated, nil
}

func ValidateSemanticRoutePath(rawPath string) (tree.RoutePath, error) {
	ve := sharederrors.NewValidationErrors()
	cleanPath := strings.Trim(strings.TrimSpace(rawPath), "/")
	if cleanPath == "" {
		ve.AddWithCode("path", FieldCodePagePathRequired, MessageIDPagePathRequired)
		return "", ve
	}
	routePath, err := tree.ParseRoutePath(cleanPath)
	if err != nil {
		ve.AddWithCode("path", FieldCodePagePathInvalid, MessageIDPagePathInvalid)
		return "", ve
	}
	return routePath, nil
}

func ValidateRoutePathValue(path tree.RoutePath) (tree.RoutePath, error) {
	routePath := path.Clean()
	if routePath.IsRoot() {
		ve := sharederrors.NewValidationErrors()
		ve.AddWithCode("path", FieldCodePagePathRequired, MessageIDPagePathRequired)
		return "", ve
	}
	routePath, err := routePath.Validate()
	if err != nil {
		ve := sharederrors.NewValidationErrors()
		ve.AddWithCode("path", FieldCodePagePathInvalid, MessageIDPagePathInvalid)
		return "", ve
	}
	return routePath, nil
}

func optionalPageIDString(id *tree.PageID) *string {
	if id == nil {
		return nil
	}
	raw := id.MetadataValue()
	return &raw
}

package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/core/markdown"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

const (
	pageVersionConflictMessage           = "Page was changed by another request"
	pageVersionConflictTemplate          = "page was changed by another request"
	pageVersionRequiredMessage           = "Page version is required"
	pageVersionRequiredTemplate          = "page version is required"
	partialEditVersionDiagnosticTemplate = "currentPageId=%s currentPath=%s currentTitle=%s currentVersion=%s"
)

func (r *Routes) registerPartialEditTools(server *sdkmcp.Server) {
	addEditorTool[updatePageMetadataInput, partialEditOutput](r, server, toolUpdatePageMetadata, func(ctx context.Context, actor toolActor, in updatePageMetadataInput) (partialEditOutput, error) {
		page, err := r.resolveValidationPage(ctx, validatePageInput{PageID: in.PageID, Path: in.Path})
		if err != nil {
			return partialEditOutput{}, err
		}
		if err := partialEditVersionPreflight(tree.NewPageVersionUnchecked(strings.TrimSpace(in.Version)), page); err != nil {
			return partialEditOutput{}, err
		}
		raw, err := r.treeService.ReadPageRaw(page.ID)
		if err != nil {
			return partialEditOutput{}, err
		}
		doc, _, err := markdown.ParsePageDocument(raw)
		if err != nil {
			return partialEditOutput{}, err
		}
		currentTags, currentProperties := pages.ExtractPageMetadataFromPageMetadata(doc.Metadata)
		tags, properties, err := pages.ApplyMetadataPatch(currentTags, currentProperties, pages.MetadataPatch{
			SetTags:          in.SetTags,
			AddTags:          in.AddTags,
			RemoveTags:       in.RemoveTags,
			SetProperties:    in.SetProperties,
			RemoveProperties: in.RemoveProperties,
		})
		if err != nil {
			return partialEditOutput{}, err
		}
		combined, err := pages.BuildMarkdownWithPublicMetadataPatch(raw, page.ID, page.Title, pages.PublicMetadataPatch{
			Tags:              tags,
			TagsPresent:       true,
			Properties:        properties,
			PropertiesPresent: true,
		}, doc.Body)
		if err != nil {
			return partialEditOutput{}, err
		}
		kind := tree.NodeKindPage
		out, err := r.updatePage.Execute(ctx, pages.UpdatePageInput{
			UserID:     tree.NewUserIDUnchecked(actor.ID),
			Source:     pagesave.PageMutationSourceMCP,
			ID:         page.ID,
			Version:    tree.NewPageVersionUnchecked(strings.TrimSpace(in.Version)),
			Title:      page.Title,
			Slug:       page.Slug,
			Content:    &combined,
			Kind:       &kind,
			FromImport: true,
		})
		if err != nil {
			return partialEditOutput{}, partialEditWriteError(err, page)
		}
		return r.partialEditOutput(ctx, out.Page, in.IncludePage, in.IncludeValidation, in.IncludeLinkStatus)
	})

	addEditorTool[replacePageSectionInput, partialEditOutput](r, server, toolReplacePageSection, func(ctx context.Context, actor toolActor, in replacePageSectionInput) (partialEditOutput, error) {
		page, err := r.resolveValidationPage(ctx, validatePageInput{PageID: in.PageID, Path: in.Path})
		if err != nil {
			return partialEditOutput{}, err
		}
		if err := partialEditVersionPreflight(tree.NewPageVersionUnchecked(strings.TrimSpace(in.Version)), page); err != nil {
			return partialEditOutput{}, err
		}
		content, err := pages.ReplaceMarkdownSection(page.Content, in.HeadingPath, in.Occurrence, in.Content)
		if err != nil {
			return partialEditOutput{}, err
		}
		kind := tree.NodeKindPage
		out, err := r.updatePage.Execute(ctx, pages.UpdatePageInput{
			UserID:     tree.NewUserIDUnchecked(actor.ID),
			Source:     pagesave.PageMutationSourceMCP,
			ID:         page.ID,
			Version:    tree.NewPageVersionUnchecked(strings.TrimSpace(in.Version)),
			Title:      page.Title,
			Slug:       page.Slug,
			Content:    &content,
			Kind:       &kind,
			FromImport: false,
		})
		if err != nil {
			return partialEditOutput{}, partialEditWriteError(err, page)
		}
		return r.partialEditOutput(ctx, out.Page, in.IncludePage, in.IncludeValidation, in.IncludeLinkStatus)
	})
}

func partialEditVersionPreflight(requested tree.PageVersion, page *tree.Page) error {
	if page == nil {
		return nil
	}
	current := page.Version()
	if current == "" {
		return nil
	}
	if requested == "" || requested.IsUnchecked() {
		return partialEditWriteError(tree.ErrVersionRequired, page)
	}
	if requested != current {
		return partialEditWriteError(tree.ErrVersionConflict, page)
	}
	return nil
}

func partialEditWriteError(err error, page *tree.Page) error {
	if page == nil {
		return err
	}
	var code sharederrors.ErrorCode
	message := ""
	template := ""
	switch {
	case errors.Is(err, tree.ErrVersionConflict):
		code = pages.ErrCodePageVersionConflict
		message = pageVersionConflictMessage
		template = pageVersionConflictTemplate
	case errors.Is(err, tree.ErrVersionRequired):
		code = pages.ErrCodePageVersionRequired
		message = pageVersionRequiredMessage
		template = pageVersionRequiredTemplate
	default:
		return err
	}
	cause := fmt.Errorf(
		partialEditVersionDiagnosticTemplate,
		page.ID,
		strings.Trim(page.CalculatePath(), "/"),
		page.Title,
		page.Version(),
	)
	return sharederrors.NewLocalizedErrorFromCodeWithFallback(code, message, template, cause)
}

func (r *Routes) partialEditOutput(ctx context.Context, page *tree.Page, includePage bool, includeValidationValue *bool, includeLinkStatus bool) (partialEditOutput, error) {
	updatedPage := r.apiPage(page, 0)
	result := partialEditOutput{
		PageID:  updatedPage.ID,
		Path:    updatedPage.Path,
		Title:   updatedPage.Title,
		Version: updatedPage.Version,
	}
	if includeValidation(includeValidationValue) {
		raw, err := r.treeService.ReadPageRaw(tree.NewPageIDUnchecked(updatedPage.ID))
		if err != nil {
			return partialEditOutput{}, err
		}
		validationRoutePath := tree.NewRoutePathUnchecked(strings.Trim(updatedPage.Path, "/"))
		validation := validationOutputFromResult(r.validateMarkdownContent(ctx, validationRoutePath, raw, tree.NewPageIDUnchecked(updatedPage.ID), page.Kind))
		result.Validation = &validation
	}
	if includePage {
		result.Page = updatedPage
	}
	if includeLinkStatus {
		withLinks, err := r.pageOutputWithLinkStatus(ctx, page, 0)
		if err != nil {
			return partialEditOutput{}, err
		}
		result.LinkStatus = withLinks.LinkStatus
	}
	return result, nil
}

func includeValidation(raw *bool) bool {
	return raw == nil || *raw
}

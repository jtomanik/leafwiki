package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/wiki/pages"
	"github.com/perber/wiki/internal/wiki/pagesave"
)

func (r *Routes) registerPartialEditTools(server *sdkmcp.Server) {
	addEditorTool[updatePageMetadataInput, partialEditOutput](r, server, toolUpdatePageMetadata, func(ctx context.Context, actor toolActor, in updatePageMetadataInput) (partialEditOutput, error) {
		page, err := r.resolveValidationPage(ctx, validatePageInput{PageID: in.PageID, Path: in.Path})
		if err != nil {
			return partialEditOutput{}, err
		}
		if err := partialEditVersionPreflight(strings.TrimSpace(in.Version), page); err != nil {
			return partialEditOutput{}, err
		}
		raw, err := r.treeService.ReadPageRaw(page.ID)
		if err != nil {
			return partialEditOutput{}, err
		}
		fm, body, hasFrontmatter, err := markdown.ParseFrontmatter(raw)
		if err != nil {
			return partialEditOutput{}, err
		}
		if !hasFrontmatter {
			body = raw
		}
		currentTags, currentProperties := pages.ExtractPageMetadata(fm.ExtraFields)
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
		fm.ExtraFields = patchMetadataExtraFields(fm.ExtraFields, currentProperties, tags, properties)
		combined, err := markdown.BuildMarkdownWithFrontmatter(fm, body)
		if err != nil {
			return partialEditOutput{}, err
		}
		kind := tree.NodeKindPage
		out, err := r.updatePage.Execute(ctx, pages.UpdatePageInput{
			UserID:     actor.ID,
			Source:     pagesave.PageMutationSourceMCP,
			ID:         page.ID,
			Version:    strings.TrimSpace(in.Version),
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
		if err := partialEditVersionPreflight(strings.TrimSpace(in.Version), page); err != nil {
			return partialEditOutput{}, err
		}
		content, err := pages.ReplaceMarkdownSection(page.Content, in.HeadingPath, in.Occurrence, in.Content)
		if err != nil {
			return partialEditOutput{}, err
		}
		kind := tree.NodeKindPage
		out, err := r.updatePage.Execute(ctx, pages.UpdatePageInput{
			UserID:     actor.ID,
			Source:     pagesave.PageMutationSourceMCP,
			ID:         page.ID,
			Version:    strings.TrimSpace(in.Version),
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

func partialEditVersionPreflight(version string, page *tree.Page) error {
	if page == nil {
		return nil
	}
	current := page.Version()
	if current == "" {
		return nil
	}
	if version == "" || version == tree.VersionUnchecked {
		return partialEditWriteError(tree.ErrVersionRequired, page)
	}
	if version != current {
		return partialEditWriteError(tree.ErrVersionConflict, page)
	}
	return nil
}

func partialEditWriteError(err error, page *tree.Page) error {
	if page == nil {
		return err
	}
	code := ""
	message := ""
	switch {
	case errors.Is(err, tree.ErrVersionConflict):
		code = pages.ErrCodePageVersionConflict
		message = "Page was changed by another request"
	case errors.Is(err, tree.ErrVersionRequired):
		code = pages.ErrCodePageVersionRequired
		message = "Page version is required"
	default:
		return err
	}
	return fmt.Errorf("%s: %s (currentPageId=%s currentPath=%s currentTitle=%s currentVersion=%s)",
		code,
		message,
		page.ID,
		strings.Trim(page.CalculatePath(), "/"),
		page.Title,
		page.Version(),
	)
}

func patchMetadataExtraFields(current map[string]interface{}, currentProperties map[string]string, tags []string, properties map[string]string) map[string]interface{} {
	next := make(map[string]interface{}, len(current)+len(properties)+1)
	for key, value := range current {
		lower := strings.ToLower(strings.TrimSpace(key))
		if lower == "tags" {
			continue
		}
		if _, isProperty := currentProperties[key]; isProperty {
			continue
		}
		next[key] = value
	}
	if len(tags) > 0 {
		next["tags"] = append([]string{}, tags...)
	}
	for key, value := range properties {
		next[key] = value
	}
	if len(next) == 0 {
		return nil
	}
	return next
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
		raw, err := r.treeService.ReadPageRaw(updatedPage.ID)
		if err != nil {
			return partialEditOutput{}, err
		}
		validation := validationOutputFromResult(r.validateMarkdownContent(ctx, updatedPage.Path, raw, updatedPage.ID, page.Kind))
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

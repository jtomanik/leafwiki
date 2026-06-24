package revisions

import (
	"strings"

	"github.com/perber/wiki/internal/core/revision"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

func ValidateRevisionLookupInput(rawPageID, rawRevisionID string) (tree.PageID, revision.RevisionID, error) {
	return ValidateRevisionLookup(tree.NewPageIDUnchecked(strings.TrimSpace(rawPageID)), rawRevisionID)
}

func ValidateRevisionLookup(pageID tree.PageID, rawRevisionID string) (tree.PageID, revision.RevisionID, error) {
	revisionID := revision.NewRevisionIDUnchecked(strings.TrimSpace(rawRevisionID))
	if pageID == "" {
		return "", "", sharederrors.NewLocalizedErrorFromCode(ErrCodeRevisionInvalidPageID, nil)
	}
	if revisionID == "" {
		return "", "", sharederrors.NewLocalizedErrorFromCode(ErrCodeRevisionInvalidRevisionID, nil)
	}
	return pageID, revisionID, nil
}

func ValidateRevisionCompareInput(rawPageID, rawBaseRevisionID, rawTargetRevisionID string) (tree.PageID, revision.RevisionID, revision.RevisionID, error) {
	return ValidateRevisionCompare(tree.NewPageIDUnchecked(strings.TrimSpace(rawPageID)), rawBaseRevisionID, rawTargetRevisionID)
}

func ValidateRevisionCompare(pageID tree.PageID, rawBaseRevisionID, rawTargetRevisionID string) (tree.PageID, revision.RevisionID, revision.RevisionID, error) {
	baseRevisionID := revision.NewRevisionIDUnchecked(strings.TrimSpace(rawBaseRevisionID))
	targetRevisionID := revision.NewRevisionIDUnchecked(strings.TrimSpace(rawTargetRevisionID))
	if pageID == "" {
		return "", "", "", sharederrors.NewLocalizedErrorFromCode(ErrCodeRevisionInvalidPageID, nil)
	}
	if baseRevisionID == "" || targetRevisionID == "" {
		return "", "", "", sharederrors.NewLocalizedErrorFromCode(ErrCodeRevisionCompareInvalidRequest, nil, pageID.MetadataValue())
	}
	return pageID, baseRevisionID, targetRevisionID, nil
}

func ValidateRevisionAssetInput(rawPageID string, rawRevisionID string, rawAssetName string) (tree.PageID, revision.RevisionID, tree.AssetName, error) {
	return ValidateRevisionAsset(tree.NewPageIDUnchecked(strings.TrimSpace(rawPageID)), rawRevisionID, rawAssetName)
}

func ValidateRevisionAsset(pageID tree.PageID, rawRevisionID string, rawAssetName string) (tree.PageID, revision.RevisionID, tree.AssetName, error) {
	pageID, revisionID, err := ValidateRevisionLookup(pageID, rawRevisionID)
	if err != nil {
		return "", "", "", err
	}
	assetName := tree.NewAssetNameUnchecked(strings.TrimSpace(strings.TrimPrefix(rawAssetName, "/")))
	if assetName == "" {
		return "", "", "", sharederrors.NewLocalizedErrorFromCode(ErrCodeRevisionPreviewAssetInvalidName, nil, pageID.MetadataValue(), revisionID.CommitID())
	}
	return pageID, revisionID, assetName, nil
}

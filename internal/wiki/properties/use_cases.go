package properties

import (
	"context"
	"strings"

	"github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
	"github.com/perber/wiki/internal/http/dto"
	coreprop "github.com/perber/wiki/internal/properties"
)

// ─── Sentinel errors ─────────────────────────────────────────────────────────

var ErrPropertiesMissingKey = sharederrors.NewLocalizedErrorFromCode(ErrCodePropertiesMissingKey, nil)

var ErrPropertiesMissingValue = sharederrors.NewLocalizedErrorFromCode(ErrCodePropertiesMissingValue, nil)

// ─── GetPropertyKeysUseCase ──────────────────────────────────────────────────

type GetPropertyKeysInput struct {
	Filter   string
	PageSize coreprop.PropertyKeyLimit
}

type GetPropertyKeysOutput struct {
	Keys []coreprop.PropertyKeyCount
}

type GetPropertyKeysUseCase struct {
	svc *coreprop.PropertiesService
}

func NewGetPropertyKeysUseCase(svc *coreprop.PropertiesService) *GetPropertyKeysUseCase {
	return &GetPropertyKeysUseCase{svc: svc}
}

func (uc *GetPropertyKeysUseCase) Execute(_ context.Context, in GetPropertyKeysInput) (*GetPropertyKeysOutput, error) {
	pageSize := in.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}

	keys, err := uc.svc.GetAllPropertyKeys(strings.ToLower(strings.TrimSpace(in.Filter)), pageSize)
	if err != nil {
		return nil, err
	}
	if keys == nil {
		keys = []coreprop.PropertyKeyCount{}
	}
	return &GetPropertyKeysOutput{Keys: keys}, nil
}

// ─── GetPagesByPropertyUseCase ───────────────────────────────────────────────

type GetPagesByPropertyInput struct {
	Key   string
	Value string
}

type GetPagesByPropertyOutput struct {
	Pages []*dto.PropertyPage
}

type GetPagesByPropertyUseCase struct {
	svc          *coreprop.PropertiesService
	treeService  *tree.TreeService
	userResolver *auth.UserResolver
}

func NewGetPagesByPropertyUseCase(svc *coreprop.PropertiesService, treeService *tree.TreeService, userResolver *auth.UserResolver) *GetPagesByPropertyUseCase {
	return &GetPagesByPropertyUseCase{svc: svc, treeService: treeService, userResolver: userResolver}
}

func (uc *GetPagesByPropertyUseCase) Execute(_ context.Context, in GetPagesByPropertyInput) (*GetPagesByPropertyOutput, error) {
	if strings.TrimSpace(in.Key) == "" {
		return nil, ErrPropertiesMissingKey
	}
	if strings.TrimSpace(in.Value) == "" {
		return nil, ErrPropertiesMissingValue
	}

	pageIDs, err := uc.svc.GetPageIDsByProperty(in.Key, in.Value)
	if err != nil {
		return nil, err
	}
	if len(pageIDs) == 0 {
		return &GetPagesByPropertyOutput{Pages: []*dto.PropertyPage{}}, nil
	}

	propsPerPage, err := uc.svc.GetPropertiesForPages(pageIDs)
	if err != nil {
		return nil, err
	}

	pages := make([]*dto.PropertyPage, 0, len(pageIDs))
	for _, id := range pageIDs {
		node, err := uc.treeService.FindPageByID(id)
		if err != nil || node == nil {
			continue
		}
		pages = append(pages, dto.ToPropertyPage(node, propsPerPage[id], uc.userResolver))
	}

	return &GetPagesByPropertyOutput{Pages: pages}, nil
}

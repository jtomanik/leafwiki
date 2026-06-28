package branding

import (
	"context"
	"mime/multipart"

	corebanding "github.com/perber/wiki/internal/branding"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/core/tree"
)

// ─── GetBrandingUseCase ──────────────────────────────────────────────────────

type GetBrandingOutput struct {
	Config *corebanding.BrandingConfigResponse
}

type brandingServiceClient interface {
	GetBranding() (*corebanding.BrandingConfigResponse, error)
	UpdateBranding(siteName string) error
	UploadLogo(file multipart.File, filename string) (string, error)
	DeleteLogo() error
	UploadFavicon(file multipart.File, filename string) (string, error)
	DeleteFavicon() error
	GetBrandingAssetsDir() string
}

type GetBrandingUseCase struct {
	branding brandingServiceClient
}

func NewGetBrandingUseCase(b *corebanding.BrandingService) *GetBrandingUseCase {
	return &GetBrandingUseCase{branding: b}
}

func (uc *GetBrandingUseCase) Execute(_ context.Context) (*GetBrandingOutput, error) {
	cfg, err := uc.branding.GetBranding()
	if err != nil {
		return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingConfigUnavailable, err)
	}
	return &GetBrandingOutput{Config: cfg}, nil
}

// ─── UpdateBrandingUseCase ───────────────────────────────────────────────────

type UpdateBrandingInput struct {
	SiteName string
}

type UpdateBrandingUseCase struct {
	branding brandingServiceClient
}

func NewUpdateBrandingUseCase(b *corebanding.BrandingService) *UpdateBrandingUseCase {
	return &UpdateBrandingUseCase{branding: b}
}

func (uc *UpdateBrandingUseCase) Execute(_ context.Context, in UpdateBrandingInput) (*GetBrandingOutput, error) {
	if err := uc.branding.UpdateBranding(in.SiteName); err != nil {
		return nil, err
	}
	cfg, err := uc.branding.GetBranding()
	if err != nil {
		return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingConfigUnavailable, err)
	}
	return &GetBrandingOutput{Config: cfg}, nil
}

// ─── UploadLogoUseCase ───────────────────────────────────────────────────────

type UploadLogoInput struct {
	File     multipart.File
	Filename tree.AssetName
}

type UploadLogoOutput struct {
	Path   string
	Config *corebanding.BrandingConfigResponse
}

type UploadLogoUseCase struct {
	branding brandingServiceClient
}

func NewUploadLogoUseCase(b *corebanding.BrandingService) *UploadLogoUseCase {
	return &UploadLogoUseCase{branding: b}
}

func (uc *UploadLogoUseCase) Execute(_ context.Context, in UploadLogoInput) (*UploadLogoOutput, error) {
	path, err := uc.branding.UploadLogo(in.File, in.Filename.Filename())
	if err != nil {
		return nil, err
	}
	cfg, err := uc.branding.GetBranding()
	if err != nil {
		return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingConfigUnavailable, err)
	}
	return &UploadLogoOutput{Path: path, Config: cfg}, nil
}

// ─── DeleteLogoUseCase ───────────────────────────────────────────────────────

type DeleteLogoUseCase struct {
	branding brandingServiceClient
}

func NewDeleteLogoUseCase(b *corebanding.BrandingService) *DeleteLogoUseCase {
	return &DeleteLogoUseCase{branding: b}
}

func (uc *DeleteLogoUseCase) Execute(_ context.Context) (*GetBrandingOutput, error) {
	if err := uc.branding.DeleteLogo(); err != nil {
		return nil, err
	}
	cfg, err := uc.branding.GetBranding()
	if err != nil {
		return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingConfigUnavailable, err)
	}
	return &GetBrandingOutput{Config: cfg}, nil
}

// ─── UploadFaviconUseCase ────────────────────────────────────────────────────

type UploadFaviconInput struct {
	File     multipart.File
	Filename tree.AssetName
}

type UploadFaviconOutput struct {
	Path   string
	Config *corebanding.BrandingConfigResponse
}

type UploadFaviconUseCase struct {
	branding brandingServiceClient
}

func NewUploadFaviconUseCase(b *corebanding.BrandingService) *UploadFaviconUseCase {
	return &UploadFaviconUseCase{branding: b}
}

func (uc *UploadFaviconUseCase) Execute(_ context.Context, in UploadFaviconInput) (*UploadFaviconOutput, error) {
	path, err := uc.branding.UploadFavicon(in.File, in.Filename.Filename())
	if err != nil {
		return nil, err
	}
	cfg, err := uc.branding.GetBranding()
	if err != nil {
		return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingConfigUnavailable, err)
	}
	return &UploadFaviconOutput{Path: path, Config: cfg}, nil
}

// ─── DeleteFaviconUseCase ────────────────────────────────────────────────────

type DeleteFaviconUseCase struct {
	branding brandingServiceClient
}

func NewDeleteFaviconUseCase(b *corebanding.BrandingService) *DeleteFaviconUseCase {
	return &DeleteFaviconUseCase{branding: b}
}

func (uc *DeleteFaviconUseCase) Execute(_ context.Context) (*GetBrandingOutput, error) {
	if err := uc.branding.DeleteFavicon(); err != nil {
		return nil, err
	}
	cfg, err := uc.branding.GetBranding()
	if err != nil {
		return nil, sharederrors.NewLocalizedErrorFromCode(ErrCodeBrandingConfigUnavailable, err)
	}
	return &GetBrandingOutput{Config: cfg}, nil
}

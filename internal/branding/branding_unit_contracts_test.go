package branding

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

var _ = Describe("branding package contracts", Label("unit"), func() {
	It("projects sorted asset constraints and extension policy from the default config", func() {
		config := DefaultBrandingConfig()

		Expect(config.AllowedLogoExts()).To(Equal([]string{".jpeg", ".jpg", ".png", ".webp"}))
		Expect(config.AllowedLogoExtsAsString()).To(Equal(".jpeg,.jpg,.png,.webp"))
		Expect(config.AllowedFaviconExts()).To(Equal([]string{".gif", ".ico", ".png", ".webp"}))
		Expect(config.AllowedFaviconExtsAsString()).To(Equal(".gif,.ico,.png,.webp"))
		Expect(brandingExtensionPolicyFor(config, "LOGO.PNG", "favicon.ICO")).To(Equal(brandingExtensionPolicy{
			Logo:    brandingLogoExtensionAllowed,
			Favicon: brandingFaviconExtensionAllowed,
		}))
		Expect(config.ToResponse()).To(matchBrandingResponse(gstruct.Fields{
			"SiteName":    Equal(config.SiteName),
			"LogoFile":    Equal(config.LogoFile),
			"FaviconFile": Equal(config.FaviconFile),
			"BrandingConstraints": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"LogoExts":          Equal([]string{".jpeg", ".jpg", ".png", ".webp"}),
				"FaviconExts":       Equal([]string{".gif", ".ico", ".png", ".webp"}),
				"MaxLogoSize":       Equal(config.BrandingConstraints.MaxLogoSize),
				"MaxFaviconSize":    Equal(config.BrandingConstraints.MaxFaviconSize),
				"MaxSiteNameLength": Equal(config.BrandingConstraints.MaxSiteNameLength),
			}),
		}))
	})

	It("persists file-backed branding config while keeping runtime constraints derived", func() {
		dir := tempBrandingDir()
		store := NewBrandingStore(dir)

		defaultConfig, err := store.Load()
		Expect(err).To(Succeed())
		Expect(defaultConfig).To(matchBrandingConfig(gstruct.Fields{
			"SiteName": Equal(DefaultBrandingConfig().SiteName),
		}))

		config := DefaultBrandingConfig()
		config.SiteName = "Docs"
		config.LogoFile = "logo.png"
		config.FaviconFile = "favicon.ico"
		Expect(store.Save(config)).To(Succeed())
		Expect(filepath.Join(dir, "branding.json")).To(BeAnExistingFile())

		loaded, err := store.Load()
		Expect(err).To(Succeed())
		Expect(loaded).To(matchBrandingConfig(gstruct.Fields{
			"SiteName":    Equal("Docs"),
			"LogoFile":    Equal("logo.png"),
			"FaviconFile": Equal("favicon.ico"),
			"BrandingConstraints": matchBrandingConstraints(gstruct.Fields{
				"LogoExts":          HaveKey(".png"),
				"FaviconExts":       HaveKey(".ico"),
				"MaxSiteNameLength": Equal(DefaultBrandingConfig().BrandingConstraints.MaxSiteNameLength),
			}),
		}))
	})

	It("updates branding state and maintains asset files through package-owned storage", func() {
		service, dir := newTestBrandingService()
		assetsDir := filepath.Join(dir, "branding")

		Expect(service.GetBrandingAssetsDir()).To(Equal(assetsDir))
		Expect(service.UpdateBranding("  Docs \nTeam  ")).To(Succeed())
		response, err := service.GetBranding()
		Expect(err).To(Succeed())
		Expect(response).To(matchBrandingResponse(gstruct.Fields{
			"SiteName": Equal("Docs \nTeam"),
		}))

		err = service.UpdateBranding("Docs\x00Team")
		Expect(err).To(HaveBrandingSiteNameValidationError(FieldCodeBrandingSiteNameControlCharacters, MessageIDBrandingSiteNameControlCharacters))

		logo := brandingTempUpload(bytes.Repeat([]byte("l"), 16), "logo-*.PNG")
		logoName, err := service.UploadLogo(logo, "custom.PNG")
		Expect(err).To(Succeed())
		Expect(logoName).To(Equal("logo.png"))
		Expect(filepath.Join(assetsDir, "logo.png")).To(BeAnExistingFile())

		staleLogo := filepath.Join(assetsDir, "logo.webp")
		Expect(os.WriteFile(staleLogo, []byte("stale"), 0o600)).To(Succeed())
		nextLogo := brandingTempUpload(bytes.Repeat([]byte("n"), 16), "logo-*.jpg")
		logoName, err = service.UploadLogo(nextLogo, "custom.jpg")
		Expect(err).To(Succeed())
		Expect(logoName).To(Equal("logo.jpg"))
		Expect(staleLogo).NotTo(BeAnExistingFile())

		favicon := brandingTempUpload(bytes.Repeat([]byte("f"), 16), "favicon-*.ico")
		faviconName, err := service.UploadFavicon(favicon, "site.ICO")
		Expect(err).To(Succeed())
		Expect(faviconName).To(Equal("favicon.ico"))
		Expect(filepath.Join(assetsDir, "favicon.ico")).To(BeAnExistingFile())

		Expect(service.DeleteLogo()).To(Succeed())
		Expect(service.DeleteFavicon()).To(Succeed())
		finalConfig, err := NewBrandingStore(dir).Load()
		Expect(err).To(Succeed())
		Expect(finalConfig).To(matchBrandingConfig(gstruct.Fields{
			"LogoFile":    BeEmpty(),
			"FaviconFile": BeEmpty(),
		}))
	})

	It("reports branding validation, store, and asset failure contracts", func() {
		storageFile := filepath.Join(tempBrandingDir(), "storage")
		Expect(os.WriteFile(storageFile, []byte("not a directory"), 0o600)).To(Succeed())
		service, err := NewBrandingService(storageFile)
		Expect(service).To(BeNil())
		Expect(err).To(Satisfy(wrapsPathError))

		storeDir := tempBrandingDir()
		store := NewBrandingStore(storeDir)
		Expect(os.Mkdir(filepath.Join(storeDir, "branding.json"), 0o755)).To(Succeed())
		loaded, err := store.Load()
		Expect(loaded).To(BeNil())
		Expect(err).To(Satisfy(wrapsPathError))
		Expect(store.Save(DefaultBrandingConfig())).To(Satisfy(wrapsPathOrLinkError))

		parseDir := tempBrandingDir()
		Expect(os.WriteFile(filepath.Join(parseDir, "branding.json"), []byte("{broken"), 0o600)).To(Succeed())
		loaded, err = NewBrandingStore(parseDir).Load()
		Expect(loaded).To(BeNil())
		Expect(err).To(Satisfy(wrapsJSONSyntaxError))

		marshalErr := errBrandingMarshalFailed
		previousMarshalIndent := brandingMarshalIndent
		brandingMarshalIndent = func(any, string, string) ([]byte, error) {
			return nil, marshalErr
		}
		DeferCleanup(func() {
			brandingMarshalIndent = previousMarshalIndent
		})
		Expect(NewBrandingStore(tempBrandingDir()).Save(DefaultBrandingConfig())).To(MatchError(marshalErr))

		service, dir := newTestBrandingService()
		Expect(service.UpdateBranding("")).To(HaveBrandingSiteNameValidationError(FieldCodeBrandingSiteNameRequired, MessageIDBrandingSiteNameRequired))
		Expect(service.UpdateBranding(string(bytes.Repeat([]byte("x"), 101)))).To(HaveBrandingSiteNameValidationError(FieldCodeBrandingSiteNameTooLong, MessageIDBrandingSiteNameTooLong))

		logo := brandingTempUpload([]byte("logo"), "logo-*")
		logoName, err := service.UploadLogo(logo, "logo.exe")
		Expect(logoName).To(BeEmpty())
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingLogoInvalidType))

		favicon := brandingTempUpload([]byte("favicon"), "favicon-*")
		faviconName, err := service.UploadFavicon(favicon, "favicon.jpg")
		Expect(faviconName).To(BeEmpty())
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingFaviconInvalidType))

		service.brandingConfig.BrandingConstraints.MaxLogoSize = 1
		oversizedLogo := brandingTempUpload(bytes.Repeat([]byte("l"), 8), "logo-*.png")
		logoName, err = service.UploadLogo(oversizedLogo, "logo.png")
		Expect(logoName).To(BeEmpty())
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingLogoUploadFailed))

		service.brandingConfig.BrandingConstraints.MaxFaviconSize = 1
		oversizedFavicon := brandingTempUpload(bytes.Repeat([]byte("f"), 8), "favicon-*.ico")
		faviconName, err = service.UploadFavicon(oversizedFavicon, "favicon.ico")
		Expect(faviconName).To(BeEmpty())
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingFaviconUploadFailed))

		service.brandingConfig.LogoFile = "logo.png"
		Expect(os.MkdirAll(filepath.Join(dir, "branding", "logo.png", "child"), 0o755)).To(Succeed())
		Expect(service.DeleteLogo()).To(MatchBrandingLocalizedError(ErrCodeBrandingLogoDeleteFailed))

		service.brandingConfig.FaviconFile = "favicon.ico"
		Expect(os.MkdirAll(filepath.Join(dir, "branding", "favicon.ico", "child"), 0o755)).To(Succeed())
		Expect(service.DeleteFavicon()).To(MatchBrandingLocalizedError(ErrCodeBrandingFaviconDeleteFailed))

		Expect(func() {
			removeOtherMatches("[", "")
		}).NotTo(Panic())
	})
})

func matchBrandingResponse(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

var errBrandingMarshalFailed = errors.New("branding marshal failed")

type brandingLogoExtensionPolicy uint8

const (
	brandingLogoExtensionRejected brandingLogoExtensionPolicy = iota
	brandingLogoExtensionAllowed
)

type brandingFaviconExtensionPolicy uint8

const (
	brandingFaviconExtensionRejected brandingFaviconExtensionPolicy = iota
	brandingFaviconExtensionAllowed
)

type brandingExtensionPolicy struct {
	Logo    brandingLogoExtensionPolicy
	Favicon brandingFaviconExtensionPolicy
}

func brandingExtensionPolicyFor(config *BrandingConfig, logoName string, faviconName string) brandingExtensionPolicy {
	policy := brandingExtensionPolicy{
		Logo:    brandingLogoExtensionRejected,
		Favicon: brandingFaviconExtensionRejected,
	}
	if config.IsAllowedLogoExt(logoName) {
		policy.Logo = brandingLogoExtensionAllowed
	}
	if config.IsAllowedFaviconExt(faviconName) {
		policy.Favicon = brandingFaviconExtensionAllowed
	}
	return policy
}

package branding

import (
	"bytes"
	stderrors "errors"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

const brandingSiteNameValidationField testmatchers.ValidationField = "siteName"

func newTestBrandingService() (*BrandingService, string) {
	GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-branding-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, dir)

	svc, err := NewBrandingService(dir)
	Expect(err).NotTo(HaveOccurred())
	return svc, dir
}

func closeTempBrandingFile(file *os.File) {
	GinkgoHelper()
	err := file.Close()
	if err != nil && !stderrors.Is(err, os.ErrClosed) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func HaveBrandingSiteNameFieldError(code errors.FieldErrorCode, messageID errors.MessageID) types.GomegaMatcher {
	return testmatchers.ContainFieldError(brandingSiteNameValidationField, code, messageID)
}

func HaveBrandingSiteNameValidationError(code errors.FieldErrorCode, messageID errors.MessageID) types.GomegaMatcher {
	return WithTransform(func(err error) []*errors.FieldError {
		var ve *errors.ValidationErrors
		if !stderrors.As(err, &ve) {
			return nil
		}
		return ve.Errors
	}, SatisfyAll(
		HaveLen(1),
		HaveBrandingSiteNameFieldError(code, messageID),
	))
}

func HavePositiveBrandingUploadConstraints() types.GomegaMatcher {
	return SatisfyAll(
		HaveField("SiteName", Not(BeEmpty())),
		HaveField("BrandingConstraints.MaxLogoSize", BeNumerically(">", 0)),
		HaveField("BrandingConstraints.MaxFaviconSize", BeNumerically(">", 0)),
		HaveField("BrandingConstraints.LogoExts", Not(BeEmpty())),
		HaveField("BrandingConstraints.FaviconExts", Not(BeEmpty())),
	)
}

var _ = Describe("branding service", Label("integration"), func() {
	It("leaves the persisted logo reference empty when no logo is configured", func() {
		svc, dir := newTestBrandingService()

		// Ensure config persisted with empty logo
		store := NewBrandingStore(dir)
		cfg, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.LogoFile).To(BeEmpty())

		Expect(svc.DeleteLogo()).To(Succeed())

		// Still empty after delete
		cfg2, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg2.LogoFile).To(BeEmpty())

	})

	It("leaves the persisted favicon reference empty when no favicon is configured", func() {
		svc, dir := newTestBrandingService()

		store := NewBrandingStore(dir)
		cfg, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.FaviconFile).To(BeEmpty())

		Expect(svc.DeleteFavicon()).To(Succeed())

		cfg2, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg2.FaviconFile).To(BeEmpty())

	})

	It("removes the configured logo file and clears the persisted logo reference", func() {
		svc, dir := newTestBrandingService()
		assetsDir := filepath.Join(dir, "branding")

		// Seed logo file and config
		Expect(os.WriteFile(filepath.Join(assetsDir, "logo.png"), []byte("logo"), 0644)).To(Succeed())
		Expect(svc.UpdateBranding("X")).To(Succeed()) // just to ensure Save works; not required
		// Set config to reference the seeded file
		svc.mu.Lock()
		svc.brandingConfig.LogoFile = "logo.png"
		err := svc.store.Save(svc.brandingConfig)
		svc.mu.Unlock()
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.DeleteLogo()).To(Succeed())

		// File should be gone
		Expect(filepath.Join(assetsDir, "logo.png")).NotTo(BeAnExistingFile())

		// Config should be cleared on disk
		store := NewBrandingStore(dir)
		cfg, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.LogoFile).To(BeEmpty())

	})

	It("removes the configured favicon file and clears the persisted favicon reference", func() {
		svc, dir := newTestBrandingService()
		assetsDir := filepath.Join(dir, "branding")

		// Seed favicon file and config
		Expect(os.WriteFile(filepath.Join(assetsDir, "favicon.ico"), []byte("fav"), 0644)).To(Succeed())

		// Set config to reference the seeded file
		svc.mu.Lock()
		svc.brandingConfig.FaviconFile = "favicon.ico"
		err := svc.store.Save(svc.brandingConfig)
		svc.mu.Unlock()
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.DeleteFavicon()).To(Succeed())

		Expect(filepath.Join(assetsDir, "favicon.ico")).NotTo(BeAnExistingFile())

		store := NewBrandingStore(dir)
		cfg, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.FaviconFile).To(BeEmpty())

	})

	It("clears the persisted logo reference when the configured logo file is already missing", func() {
		svc, dir := newTestBrandingService()

		// Reference a file that doesn't exist
		svc.mu.Lock()
		svc.brandingConfig.LogoFile = "logo.png"
		err := svc.store.Save(svc.brandingConfig)
		svc.mu.Unlock()
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.DeleteLogo()).To(Succeed())

		store := NewBrandingStore(dir)
		cfg, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.LogoFile).To(BeEmpty())

	})

	It("clears the persisted favicon reference when the configured favicon file is already missing", func() {
		svc, dir := newTestBrandingService()

		svc.mu.Lock()
		svc.brandingConfig.FaviconFile = "favicon.ico"
		err := svc.store.Save(svc.brandingConfig)
		svc.mu.Unlock()
		Expect(err).NotTo(HaveOccurred())

		Expect(svc.DeleteFavicon()).To(Succeed())

		store := NewBrandingStore(dir)
		cfg, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.FaviconFile).To(BeEmpty())

	})

	It("stores an uploaded logo and later removes both file and persisted reference", func() {
		svc, dir := newTestBrandingService()
		assetsDir := filepath.Join(dir, "branding")

		// Upload logo.png
		tmp, err := os.CreateTemp(tempBrandingDir(), "logo-*.png")
		Expect(err).NotTo(HaveOccurred())
		Expect(tmp.Write(bytes.Repeat([]byte("a"), 64))).Error().NotTo(HaveOccurred())
		Expect(tmp.Seek(0, 0)).Error().NotTo(HaveOccurred())
		DeferCleanup(closeTempBrandingFile, tmp)

		got, err := svc.UploadLogo(tmp, "mylogo.png")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("logo.png"))
		Expect(os.Stat(filepath.Join(assetsDir, "logo.png"))).Error().NotTo(HaveOccurred())

		// Delete
		Expect(svc.DeleteLogo()).To(Succeed())
		Expect(filepath.Join(assetsDir, "logo.png")).NotTo(BeAnExistingFile())

		store := NewBrandingStore(dir)
		cfg, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.LogoFile).To(BeEmpty())

	})

	It("stores an uploaded favicon and later removes both file and persisted reference", func() {
		svc, dir := newTestBrandingService()
		assetsDir := filepath.Join(dir, "branding")

		tmp, err := os.CreateTemp(tempBrandingDir(), "fav-*.ico")
		Expect(err).NotTo(HaveOccurred())
		Expect(tmp.Write(bytes.Repeat([]byte("b"), 64))).Error().NotTo(HaveOccurred())
		Expect(tmp.Seek(0, 0)).Error().NotTo(HaveOccurred())
		DeferCleanup(closeTempBrandingFile, tmp)

		got, err := svc.UploadFavicon(tmp, "favicon.ico")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("favicon.ico"))
		Expect(os.Stat(filepath.Join(assetsDir, "favicon.ico"))).Error().NotTo(HaveOccurred())

		Expect(svc.DeleteFavicon()).To(Succeed())
		Expect(filepath.Join(assetsDir, "favicon.ico")).NotTo(BeAnExistingFile())

		store := NewBrandingStore(dir)
		cfg, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.FaviconFile).To(BeEmpty())

	})

	It("returns branding details with positive upload constraints", func() {
		svc, _ := newTestBrandingService()

		resp, err := svc.GetBranding()
		Expect(err).NotTo(HaveOccurred())

		Expect(resp).To(HavePositiveBrandingUploadConstraints())

	})

	It("persists updated site names to disk", func() {
		svc, dir := newTestBrandingService()

		Expect(svc.UpdateBranding("My Wiki")).To(Succeed())

		// Verify persisted config by reading via store
		store := NewBrandingStore(dir)
		cfg, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.SiteName).To(Equal("My Wiki"))

	})

	It("trims surrounding whitespace before persisting site names", func() {
		svc, dir := newTestBrandingService()

		Expect(svc.UpdateBranding("  Trimmed Wiki  ")).To(Succeed())

		store := NewBrandingStore(dir)
		cfg, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.SiteName).To(Equal("Trimmed Wiki"))

	})

	It("returns a required-site-name validation error for empty site names", func() {
		svc, _ := newTestBrandingService()

		err := svc.UpdateBranding("")

		Expect(err).To(HaveBrandingSiteNameValidationError(FieldCodeBrandingSiteNameRequired, MessageIDBrandingSiteNameRequired))

	})

	It("returns a required-site-name validation error for whitespace-only site names", func() {
		svc, _ := newTestBrandingService()

		err := svc.UpdateBranding("   ")

		Expect(err).To(HaveBrandingSiteNameValidationError(FieldCodeBrandingSiteNameRequired, MessageIDBrandingSiteNameRequired))

	})

	It("returns a too-long validation error when the site name exceeds the configured length", func() {
		svc, _ := newTestBrandingService()

		// Create a site name that exceeds the max length (default is 100)
		longName := strings.Repeat("a", 101)

		err := svc.UpdateBranding(longName)

		Expect(err).To(HaveBrandingSiteNameValidationError(FieldCodeBrandingSiteNameTooLong, MessageIDBrandingSiteNameTooLong))

	})

	It("persists site names at the maximum configured length", func() {
		svc, dir := newTestBrandingService()

		// Create a site name exactly at max length (default is 100)
		exactName := strings.Repeat("a", 100)

		Expect(svc.UpdateBranding(exactName)).To(Succeed())

		store := NewBrandingStore(dir)
		cfg, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.SiteName).To(Equal(exactName))

	})

	It("returns a control-character validation error for unsafe site names", func() {
		svc, _ := newTestBrandingService()

		// Test with null character (control character)
		nameWithControl := "My\x00Wiki"

		err := svc.UpdateBranding(nameWithControl)

		Expect(err).To(HaveBrandingSiteNameValidationError(FieldCodeBrandingSiteNameControlCharacters, MessageIDBrandingSiteNameControlCharacters))

	})

	It("persists site names containing common special characters", func() {
		svc, dir := newTestBrandingService()

		// Test with common special characters that should be allowed
		validName := "My Wiki - The Best! (2024) & More"

		Expect(svc.UpdateBranding(validName)).To(Succeed())

		store := NewBrandingStore(dir)
		cfg, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.SiteName).To(Equal(validName))

	})

	It("returns a localized logo type error for unsupported logo extensions", func() {
		svc, _ := newTestBrandingService()

		// bytes.Reader implements io.Reader, but UploadLogo expects multipart.File.
		// multipart.File is an interface satisfied by *os.File and multipart.SectionReadCloser.
		// We'll use an actual temp file.
		f, err := os.CreateTemp(tempBrandingDir(), "badlogo-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(closeTempBrandingFile, f)

		_, err = svc.UploadLogo(f, "logo.exe")
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingLogoInvalidType))

	})

	It("returns a localized favicon type error for unsupported favicon extensions", func() {
		svc, _ := newTestBrandingService()

		f, err := os.CreateTemp(tempBrandingDir(), "badfav-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(closeTempBrandingFile, f)

		_, err = svc.UploadFavicon(f, "favicon.jpg") // jpg should be invalid for favicon in defaults
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingFaviconInvalidType))

	})

	It("writes uploaded logo files and persists the logo reference", func() {
		svc, dir := newTestBrandingService()

		// Create a small "image" file (content doesn't matter; size and extension do)
		content := bytes.Repeat([]byte("a"), 128)
		tmp, err := os.CreateTemp(tempBrandingDir(), "logo-*.png")
		Expect(err).NotTo(HaveOccurred())
		Expect(tmp.Write(content)).Error().NotTo(HaveOccurred())
		Expect(tmp.Seek(0, 0)).Error().NotTo(HaveOccurred())
		DeferCleanup(closeTempBrandingFile, tmp)

		rel, err := svc.UploadLogo(tmp, "mylogo.png")
		Expect(err).NotTo(HaveOccurred())
		Expect(rel).To(Equal("logo.png"))

		// File should exist under branding assets dir
		assetsDir := filepath.Join(dir, "branding")
		target := filepath.Join(assetsDir, "logo.png")
		Expect(target).To(BeAnExistingFile())

		// Config should be updated and persisted
		store := NewBrandingStore(dir)
		cfg, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.LogoFile).To(Equal("logo.png"))

	})

	It("writes uploaded favicon files and persists the favicon reference", func() {
		svc, dir := newTestBrandingService()

		content := bytes.Repeat([]byte("b"), 128)
		tmp, err := os.CreateTemp(tempBrandingDir(), "favicon-*.ico")
		Expect(err).NotTo(HaveOccurred())
		Expect(tmp.Write(content)).Error().NotTo(HaveOccurred())
		Expect(tmp.Seek(0, 0)).Error().NotTo(HaveOccurred())
		DeferCleanup(closeTempBrandingFile, tmp)

		rel, err := svc.UploadFavicon(tmp, "favicon.ico")
		Expect(err).NotTo(HaveOccurred())
		Expect(rel).To(Equal("favicon.ico"))

		assetsDir := filepath.Join(dir, "branding")
		target := filepath.Join(assetsDir, "favicon.ico")
		Expect(target).To(BeAnExistingFile())

		store := NewBrandingStore(dir)
		cfg, err := store.Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.FaviconFile).To(Equal("favicon.ico"))

	})

	It("removes stale logo variants when a new logo is uploaded", func() {
		svc, dir := newTestBrandingService()

		assetsDir := filepath.Join(dir, "branding")

		// Seed old variants
		Expect(os.WriteFile(filepath.Join(assetsDir, "logo.jpg"), []byte("old"), 0644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(assetsDir, "logo.webp"), []byte("old"), 0644)).To(Succeed())

		// Upload new logo.png
		tmp, err := os.CreateTemp(tempBrandingDir(), "logo-*.png")
		Expect(err).NotTo(HaveOccurred())
		Expect(tmp.Write([]byte("new"))).Error().NotTo(HaveOccurred())
		Expect(tmp.Seek(0, 0)).Error().NotTo(HaveOccurred())
		DeferCleanup(closeTempBrandingFile, tmp)

		Expect(svc.UploadLogo(tmp, "logo.png")).Error().NotTo(HaveOccurred())

		// New should exist
		Expect(os.Stat(filepath.Join(assetsDir, "logo.png"))).Error().NotTo(HaveOccurred())
		// Old should be removed
		Expect(filepath.Join(assetsDir, "logo.jpg")).NotTo(BeAnExistingFile())
		Expect(filepath.Join(assetsDir, "logo.webp")).NotTo(BeAnExistingFile())

	})

	It("removes stale favicon variants when a new favicon is uploaded", func() {
		svc, dir := newTestBrandingService()

		assetsDir := filepath.Join(dir, "branding")

		// Seed old variants
		Expect(os.WriteFile(filepath.Join(assetsDir, "favicon.png"), []byte("old"), 0644)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(assetsDir, "favicon.webp"), []byte("old"), 0644)).To(Succeed())

		// Upload new favicon.ico
		tmp, err := os.CreateTemp(tempBrandingDir(), "favicon-*.ico")
		Expect(err).NotTo(HaveOccurred())
		Expect(tmp.Write([]byte("new"))).Error().NotTo(HaveOccurred())
		Expect(tmp.Seek(0, 0)).Error().NotTo(HaveOccurred())
		DeferCleanup(closeTempBrandingFile, tmp)

		Expect(svc.UploadFavicon(tmp, "favicon.ico")).Error().NotTo(HaveOccurred())

		// New should exist
		Expect(os.Stat(filepath.Join(assetsDir, "favicon.ico"))).Error().NotTo(HaveOccurred())
		// Old should be removed
		Expect(filepath.Join(assetsDir, "favicon.png")).NotTo(BeAnExistingFile())
		Expect(filepath.Join(assetsDir, "favicon.webp")).NotTo(BeAnExistingFile())

	})

	It("rejects oversized logos without updating persisted branding config", func() {
		svc, dir := newTestBrandingService()

		// Lower max size to make test fast
		svc.brandingConfig.BrandingConstraints.MaxLogoSize = 10

		// Create file > 10 bytes
		tmp, err := os.CreateTemp(tempBrandingDir(), "logo-big-*.png")
		Expect(err).NotTo(HaveOccurred())
		Expect(tmp.Write(bytes.Repeat([]byte("x"), 50))).Error().NotTo(HaveOccurred())
		Expect(tmp.Seek(0, 0)).Error().NotTo(HaveOccurred())
		DeferCleanup(closeTempBrandingFile, tmp)

		_, err = svc.UploadLogo(tmp, "logo.png")
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingLogoUploadFailed))

		// Should not have updated persisted config
		store := NewBrandingStore(dir)
		cfg, err2 := store.Load()
		Expect(err2).NotTo(HaveOccurred())
		Expect(cfg.LogoFile).To(BeEmpty())

	})

	It("rejects oversized favicons without updating persisted branding config", func() {
		svc, dir := newTestBrandingService()

		// Lower max size to make test fast
		svc.brandingConfig.BrandingConstraints.MaxFaviconSize = 10

		tmp, err := os.CreateTemp(tempBrandingDir(), "fav-big-*.ico")
		Expect(err).NotTo(HaveOccurred())
		Expect(tmp.Write(bytes.Repeat([]byte("y"), 50))).Error().NotTo(HaveOccurred())
		Expect(tmp.Seek(0, 0)).Error().NotTo(HaveOccurred())
		DeferCleanup(closeTempBrandingFile, tmp)

		_, err = svc.UploadFavicon(tmp, "favicon.ico")
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingFaviconUploadFailed))

		store := NewBrandingStore(dir)
		cfg, err2 := store.Load()
		Expect(err2).NotTo(HaveOccurred())
		Expect(cfg.FaviconFile).To(BeEmpty())

	})
})

package branding

import (
	"bytes"
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

var _ = It("UpdateBranding accepts a trimmed site name at the maximum length", func() {
	svc, dir := newTestBrandingService(GinkgoT())
	exactName := strings.Repeat("x", 100)

	Expect(svc.UpdateBranding("  " + exactName + "  ")).To(Succeed())

	cfg, err := NewBrandingStore(dir).Load()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg.SiteName).To(Equal(exactName))
})

var _ = It("UpdateBranding allows common text whitespace in site names", func() {
	svc, dir := newTestBrandingService(GinkgoT())
	validName := "LeafWiki\tDocs\nTeam\rEdition"

	Expect(svc.UpdateBranding(validName)).To(Succeed())

	cfg, err := NewBrandingStore(dir).Load()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg.SiteName).To(Equal(validName))
})

var _ = It("UploadLogo accepts uppercase extensions and stores the normalized logo filename", func() {
	svc, dir := newTestBrandingService(GinkgoT())

	tmp, err := os.CreateTemp(GinkgoT().TempDir(), "logo-*.PNG")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { Expect(tmp.Close()).To(Succeed()) })
	_, err = tmp.Write([]byte("logo"))
	Expect(err).NotTo(HaveOccurred())
	_, err = tmp.Seek(0, 0)
	Expect(err).NotTo(HaveOccurred())

	got, err := svc.UploadLogo(tmp, "CUSTOM.PNG")
	Expect(err).NotTo(HaveOccurred())
	Expect(got).To(Equal("logo.png"))
	Expect(filepath.Join(dir, "branding", "logo.png")).To(BeAnExistingFile())
})

var _ = It("UploadFavicon accepts a file exactly at the configured size limit", func() {
	svc, dir := newTestBrandingService(GinkgoT())
	svc.brandingConfig.BrandingConstraints.MaxFaviconSize = 10

	tmp, err := os.CreateTemp(GinkgoT().TempDir(), "favicon-*.ico")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() { Expect(tmp.Close()).To(Succeed()) })
	_, err = tmp.Write(bytes.Repeat([]byte("f"), 10))
	Expect(err).NotTo(HaveOccurred())
	_, err = tmp.Seek(0, 0)
	Expect(err).NotTo(HaveOccurred())

	got, err := svc.UploadFavicon(tmp, "favicon.ico")
	Expect(err).NotTo(HaveOccurred())
	Expect(got).To(Equal("favicon.ico"))

	cfg, err := NewBrandingStore(dir).Load()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg.FaviconFile).To(Equal("favicon.ico"))
})

var _ = It("GetBrandingAssetsDir returns the package branding assets directory", func() {
	svc, dir := newTestBrandingService(GinkgoT())

	got := svc.GetBrandingAssetsDir()
	want := filepath.Join(dir, "branding")
	Expect(got).To(Equal(want))
})

var _ = It("Save returns marshal errors", func() {
	store := NewBrandingStore(GinkgoT().TempDir())
	marshalErr := stderrors.New("marshal failed")
	originalMarshalIndent := brandingMarshalIndent
	brandingMarshalIndent = func(any, string, string) ([]byte, error) {
		return nil, marshalErr
	}
	DeferCleanup(func() {
		brandingMarshalIndent = originalMarshalIndent
	})

	err := store.Save(DefaultBrandingConfig())

	Expect(err).To(MatchError(ContainSubstring("failed to marshal branding config")))
	Expect(stderrors.Is(err, marshalErr)).To(BeTrue())
})

var _ = It("NewBrandingService reports invalid persisted branding config", func() {
	dir := GinkgoT().TempDir()
	Expect(os.WriteFile(filepath.Join(dir, "branding.json"), []byte("{broken json"), 0644)).To(Succeed())

	_, err := NewBrandingService(dir)
	Expect(err).To(MatchError(ContainSubstring("failed to load branding config")))
})

var _ = Describe("branding persistence edge coverage", func() {
	It("NewBrandingService reports branding asset directory creation errors", func() {
		storageFile := filepath.Join(GinkgoT().TempDir(), "storage")
		Expect(os.WriteFile(storageFile, []byte("not a directory"), 0o600)).To(Succeed())

		svc, err := NewBrandingService(storageFile)

		Expect(svc).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("failed to create branding assets directory")))
	})

	It("BrandingStore reports read and write errors", func() {
		dir := GinkgoT().TempDir()
		store := NewBrandingStore(dir)
		Expect(os.Mkdir(filepath.Join(dir, "branding.json"), 0o755)).To(Succeed())

		loaded, err := store.Load()
		Expect(loaded).To(BeNil())
		Expect(err).To(MatchError(ContainSubstring("failed to read branding config")))

		err = store.Save(DefaultBrandingConfig())
		Expect(err).To(MatchError(ContainSubstring("failed to write branding config")))
	})

	It("UpdateBranding returns localized persistence errors", func() {
		svc, dir := newTestBrandingService(GinkgoT())
		blockBrandingConfigPath(dir)

		err := svc.UpdateBranding("LeafWiki Docs")

		expectBrandingLocalizedError(err, ErrCodeBrandingUpdateFailed)
	})

	It("UploadLogo returns localized persistence errors after writing the logo", func() {
		svc, dir := newTestBrandingService(GinkgoT())
		blockBrandingConfigPath(dir)
		logo := brandingTempUpload([]byte("logo"), "logo-*.png")

		got, err := svc.UploadLogo(logo, "logo.png")

		Expect(got).To(BeEmpty())
		expectBrandingLocalizedError(err, ErrCodeBrandingLogoUploadFailed)
		Expect(filepath.Join(dir, "branding", "logo.png")).To(BeAnExistingFile())
	})

	It("UploadFavicon returns localized persistence errors after writing the favicon", func() {
		svc, dir := newTestBrandingService(GinkgoT())
		blockBrandingConfigPath(dir)
		favicon := brandingTempUpload([]byte("favicon"), "favicon-*.ico")

		got, err := svc.UploadFavicon(favicon, "favicon.ico")

		Expect(got).To(BeEmpty())
		expectBrandingLocalizedError(err, ErrCodeBrandingFaviconUploadFailed)
		Expect(filepath.Join(dir, "branding", "favicon.ico")).To(BeAnExistingFile())
	})

	It("DeleteLogo returns localized file removal and persistence errors", func() {
		fileFailureSvc, fileFailureDir := newTestBrandingService(GinkgoT())
		fileFailureSvc.brandingConfig.LogoFile = "logo.png"
		logoDir := filepath.Join(fileFailureDir, "branding", "logo.png")
		Expect(os.MkdirAll(filepath.Join(logoDir, "child"), 0o755)).To(Succeed())

		err := fileFailureSvc.DeleteLogo()

		expectBrandingLocalizedError(err, ErrCodeBrandingLogoDeleteFailed)

		saveFailureSvc, saveFailureDir := newTestBrandingService(GinkgoT())
		saveFailureSvc.brandingConfig.LogoFile = "logo.png"
		Expect(os.WriteFile(filepath.Join(saveFailureDir, "branding", "logo.png"), []byte("logo"), 0o600)).To(Succeed())
		blockBrandingConfigPath(saveFailureDir)

		err = saveFailureSvc.DeleteLogo()

		expectBrandingLocalizedError(err, ErrCodeBrandingLogoDeleteFailed)
	})

	It("DeleteFavicon returns localized file removal and persistence errors", func() {
		fileFailureSvc, fileFailureDir := newTestBrandingService(GinkgoT())
		fileFailureSvc.brandingConfig.FaviconFile = "favicon.ico"
		faviconDir := filepath.Join(fileFailureDir, "branding", "favicon.ico")
		Expect(os.MkdirAll(filepath.Join(faviconDir, "child"), 0o755)).To(Succeed())

		err := fileFailureSvc.DeleteFavicon()

		expectBrandingLocalizedError(err, ErrCodeBrandingFaviconDeleteFailed)

		saveFailureSvc, saveFailureDir := newTestBrandingService(GinkgoT())
		saveFailureSvc.brandingConfig.FaviconFile = "favicon.ico"
		Expect(os.WriteFile(filepath.Join(saveFailureDir, "branding", "favicon.ico"), []byte("favicon"), 0o600)).To(Succeed())
		blockBrandingConfigPath(saveFailureDir)

		err = saveFailureSvc.DeleteFavicon()

		expectBrandingLocalizedError(err, ErrCodeBrandingFaviconDeleteFailed)
	})

	It("removeOtherMatches tolerates glob and remove errors", func() {
		Expect(func() {
			removeOtherMatches("[", "")
		}).NotTo(Panic())
		dir := GinkgoT().TempDir()
		keep := filepath.Join(dir, "logo.png")
		stale := filepath.Join(dir, "logo.webp")
		Expect(os.WriteFile(keep, []byte("keep"), 0o600)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(stale, "child"), 0o755)).To(Succeed())

		removeOtherMatches(filepath.Join(dir, "logo.*"), keep)

		Expect(stale).To(BeADirectory())
	})
})

func blockBrandingConfigPath(dir string) {
	GinkgoHelper()
	Expect(os.Mkdir(filepath.Join(dir, "branding.json"), 0o755)).To(Succeed())
}

func brandingTempUpload(contents []byte, pattern string) *os.File {
	GinkgoHelper()
	file, err := os.CreateTemp(GinkgoT().TempDir(), pattern)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		Expect(file.Close()).To(Succeed())
	})
	_, err = file.Write(contents)
	Expect(err).NotTo(HaveOccurred())
	_, err = file.Seek(0, 0)
	Expect(err).NotTo(HaveOccurred())
	return file
}

func expectBrandingLocalizedError(err error, code sharederrors.ErrorCode) {
	GinkgoHelper()
	var localized *sharederrors.LocalizedError
	Expect(stderrors.As(err, &localized)).To(BeTrue(), "error should be localized: %v", err)
	Expect(localized.Code).To(Equal(code))
}

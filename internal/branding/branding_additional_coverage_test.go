package branding

import (
	"bytes"
	"encoding/json"
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
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

	Expect(err).To(MatchError(marshalErr))
})

var _ = It("NewBrandingService reports invalid persisted branding config", func() {
	dir := GinkgoT().TempDir()
	Expect(os.WriteFile(filepath.Join(dir, "branding.json"), []byte("{broken json"), 0644)).To(Succeed())

	_, err := NewBrandingService(dir)
	Expect(err).To(Satisfy(wrapsJSONSyntaxError))
})

var _ = Describe("branding persistence edge coverage", func() {
	It("NewBrandingService reports branding asset directory creation errors", func() {
		storageFile := filepath.Join(GinkgoT().TempDir(), "storage")
		Expect(os.WriteFile(storageFile, []byte("not a directory"), 0o600)).To(Succeed())

		svc, err := NewBrandingService(storageFile)

		Expect(svc).To(BeNil())
		Expect(err).To(Satisfy(wrapsPathError))
	})

	It("BrandingStore reports read and write errors", func() {
		dir := GinkgoT().TempDir()
		store := NewBrandingStore(dir)
		Expect(os.Mkdir(filepath.Join(dir, "branding.json"), 0o755)).To(Succeed())

		loaded, err := store.Load()
		Expect(loaded).To(BeNil())
		Expect(err).To(Satisfy(wrapsPathError))

		err = store.Save(DefaultBrandingConfig())
		Expect(err).To(Satisfy(wrapsPathOrLinkError))
	})

	It("UpdateBranding returns localized persistence errors", func() {
		svc, dir := newTestBrandingService(GinkgoT())
		blockBrandingConfigPath(dir)

		err := svc.UpdateBranding("LeafWiki Docs")

		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingUpdateFailed))
	})

	It("UploadLogo returns localized persistence errors after writing the logo", func() {
		svc, dir := newTestBrandingService(GinkgoT())
		blockBrandingConfigPath(dir)
		logo := brandingTempUpload([]byte("logo"), "logo-*.png")

		got, err := svc.UploadLogo(logo, "logo.png")

		Expect(got).To(BeEmpty())
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingLogoUploadFailed))
		Expect(filepath.Join(dir, "branding", "logo.png")).To(BeAnExistingFile())
	})

	It("UploadFavicon returns localized persistence errors after writing the favicon", func() {
		svc, dir := newTestBrandingService(GinkgoT())
		blockBrandingConfigPath(dir)
		favicon := brandingTempUpload([]byte("favicon"), "favicon-*.ico")

		got, err := svc.UploadFavicon(favicon, "favicon.ico")

		Expect(got).To(BeEmpty())
		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingFaviconUploadFailed))
		Expect(filepath.Join(dir, "branding", "favicon.ico")).To(BeAnExistingFile())
	})

	It("DeleteLogo returns localized file removal and persistence errors", func() {
		fileFailureSvc, fileFailureDir := newTestBrandingService(GinkgoT())
		fileFailureSvc.brandingConfig.LogoFile = "logo.png"
		logoDir := filepath.Join(fileFailureDir, "branding", "logo.png")
		Expect(os.MkdirAll(filepath.Join(logoDir, "child"), 0o755)).To(Succeed())

		err := fileFailureSvc.DeleteLogo()

		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingLogoDeleteFailed))

		saveFailureSvc, saveFailureDir := newTestBrandingService(GinkgoT())
		saveFailureSvc.brandingConfig.LogoFile = "logo.png"
		Expect(os.WriteFile(filepath.Join(saveFailureDir, "branding", "logo.png"), []byte("logo"), 0o600)).To(Succeed())
		blockBrandingConfigPath(saveFailureDir)

		err = saveFailureSvc.DeleteLogo()

		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingLogoDeleteFailed))
	})

	It("DeleteFavicon returns localized file removal and persistence errors", func() {
		fileFailureSvc, fileFailureDir := newTestBrandingService(GinkgoT())
		fileFailureSvc.brandingConfig.FaviconFile = "favicon.ico"
		faviconDir := filepath.Join(fileFailureDir, "branding", "favicon.ico")
		Expect(os.MkdirAll(filepath.Join(faviconDir, "child"), 0o755)).To(Succeed())

		err := fileFailureSvc.DeleteFavicon()

		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingFaviconDeleteFailed))

		saveFailureSvc, saveFailureDir := newTestBrandingService(GinkgoT())
		saveFailureSvc.brandingConfig.FaviconFile = "favicon.ico"
		Expect(os.WriteFile(filepath.Join(saveFailureDir, "branding", "favicon.ico"), []byte("favicon"), 0o600)).To(Succeed())
		blockBrandingConfigPath(saveFailureDir)

		err = saveFailureSvc.DeleteFavicon()

		Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingFaviconDeleteFailed))
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

func MatchBrandingLocalizedError(code sharederrors.ErrorCode) types.GomegaMatcher {
	return testmatchers.MatchLocalizedError(code, sharederrors.MessageIDForCode(code))
}

func wrapsJSONSyntaxError(err error) bool {
	var syntaxErr *json.SyntaxError
	return stderrors.As(err, &syntaxErr)
}

func wrapsPathError(err error) bool {
	var pathErr *os.PathError
	return stderrors.As(err, &pathErr)
}

func wrapsPathOrLinkError(err error) bool {
	var linkErr *os.LinkError
	return wrapsPathError(err) || stderrors.As(err, &linkErr)
}

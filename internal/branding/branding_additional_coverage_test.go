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

var _ = Describe("branding service edge cases", func() {
	It("accepts a trimmed site name at the maximum length", func() {
		svc, dir := newTestBrandingService()
		exactName := strings.Repeat("x", 100)

		Expect(svc.UpdateBranding("  " + exactName + "  ")).To(Succeed())

		cfg, err := NewBrandingStore(dir).Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.SiteName).To(Equal(exactName))
	})

	It("allows common text whitespace in site names", func() {
		svc, dir := newTestBrandingService()
		validName := "LeafWiki\tDocs\nTeam\rEdition"

		Expect(svc.UpdateBranding(validName)).To(Succeed())

		cfg, err := NewBrandingStore(dir).Load()
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.SiteName).To(Equal(validName))
	})

	It("accepts uppercase logo extensions and stores the normalized logo filename", func() {
		svc, dir := newTestBrandingService()

		tmp, err := os.CreateTemp(tempBrandingDir(), "logo-*.PNG")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(closeTempBrandingFile, tmp)
		_, err = tmp.Write([]byte("logo"))
		Expect(err).NotTo(HaveOccurred())
		_, err = tmp.Seek(0, 0)
		Expect(err).NotTo(HaveOccurred())

		got, err := svc.UploadLogo(tmp, "CUSTOM.PNG")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal("logo.png"))
		Expect(filepath.Join(dir, "branding", "logo.png")).To(BeAnExistingFile())
	})

	It("accepts favicon uploads exactly at the configured size limit", func() {
		svc, dir := newTestBrandingService()
		svc.brandingConfig.BrandingConstraints.MaxFaviconSize = 10

		tmp, err := os.CreateTemp(tempBrandingDir(), "favicon-*.ico")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(closeTempBrandingFile, tmp)
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

	It("returns the package branding assets directory", func() {
		svc, dir := newTestBrandingService()

		got := svc.GetBrandingAssetsDir()
		want := filepath.Join(dir, "branding")
		Expect(got).To(Equal(want))
	})

	It("returns marshal errors from branding store saves", func() {
		store := NewBrandingStore(tempBrandingDir())
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

	It("reports invalid persisted branding config during service startup", func() {
		dir := tempBrandingDir()
		Expect(os.WriteFile(filepath.Join(dir, "branding.json"), []byte("{broken json"), 0644)).To(Succeed())

		_, err := NewBrandingService(dir)
		Expect(err).To(Satisfy(wrapsJSONSyntaxError))
	})

	Describe("persistence edge coverage", func() {
		It("reports branding asset directory creation errors during service startup", func() {
			storageFile := filepath.Join(tempBrandingDir(), "storage")
			Expect(os.WriteFile(storageFile, []byte("not a directory"), 0o600)).To(Succeed())

			svc, err := NewBrandingService(storageFile)

			Expect(svc).To(BeNil())
			Expect(err).To(Satisfy(wrapsPathError))
		})

		It("reports read and write errors from the branding store", func() {
			dir := tempBrandingDir()
			store := NewBrandingStore(dir)
			Expect(os.Mkdir(filepath.Join(dir, "branding.json"), 0o755)).To(Succeed())

			loaded, err := store.Load()
			Expect(loaded).To(BeNil())
			Expect(err).To(Satisfy(wrapsPathError))

			err = store.Save(DefaultBrandingConfig())
			Expect(err).To(Satisfy(wrapsPathOrLinkError))
		})

		It("returns localized persistence errors when updating branding fails", func() {
			svc, dir := newTestBrandingService()
			blockBrandingConfigPath(dir)

			err := svc.UpdateBranding("LeafWiki Docs")

			Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingUpdateFailed))
		})

		It("returns localized persistence errors after writing a logo", func() {
			svc, dir := newTestBrandingService()
			blockBrandingConfigPath(dir)
			logo := brandingTempUpload([]byte("logo"), "logo-*.png")

			got, err := svc.UploadLogo(logo, "logo.png")

			Expect(got).To(BeEmpty())
			Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingLogoUploadFailed))
			Expect(filepath.Join(dir, "branding", "logo.png")).To(BeAnExistingFile())
		})

		It("returns localized persistence errors after writing a favicon", func() {
			svc, dir := newTestBrandingService()
			blockBrandingConfigPath(dir)
			favicon := brandingTempUpload([]byte("favicon"), "favicon-*.ico")

			got, err := svc.UploadFavicon(favicon, "favicon.ico")

			Expect(got).To(BeEmpty())
			Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingFaviconUploadFailed))
			Expect(filepath.Join(dir, "branding", "favicon.ico")).To(BeAnExistingFile())
		})

		It("returns localized errors for logo file removal and persistence failures", func() {
			fileFailureSvc, fileFailureDir := newTestBrandingService()
			fileFailureSvc.brandingConfig.LogoFile = "logo.png"
			logoDir := filepath.Join(fileFailureDir, "branding", "logo.png")
			Expect(os.MkdirAll(filepath.Join(logoDir, "child"), 0o755)).To(Succeed())

			err := fileFailureSvc.DeleteLogo()

			Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingLogoDeleteFailed))

			saveFailureSvc, saveFailureDir := newTestBrandingService()
			saveFailureSvc.brandingConfig.LogoFile = "logo.png"
			Expect(os.WriteFile(filepath.Join(saveFailureDir, "branding", "logo.png"), []byte("logo"), 0o600)).To(Succeed())
			blockBrandingConfigPath(saveFailureDir)

			err = saveFailureSvc.DeleteLogo()

			Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingLogoDeleteFailed))
		})

		It("returns localized errors for favicon file removal and persistence failures", func() {
			fileFailureSvc, fileFailureDir := newTestBrandingService()
			fileFailureSvc.brandingConfig.FaviconFile = "favicon.ico"
			faviconDir := filepath.Join(fileFailureDir, "branding", "favicon.ico")
			Expect(os.MkdirAll(filepath.Join(faviconDir, "child"), 0o755)).To(Succeed())

			err := fileFailureSvc.DeleteFavicon()

			Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingFaviconDeleteFailed))

			saveFailureSvc, saveFailureDir := newTestBrandingService()
			saveFailureSvc.brandingConfig.FaviconFile = "favicon.ico"
			Expect(os.WriteFile(filepath.Join(saveFailureDir, "branding", "favicon.ico"), []byte("favicon"), 0o600)).To(Succeed())
			blockBrandingConfigPath(saveFailureDir)

			err = saveFailureSvc.DeleteFavicon()

			Expect(err).To(MatchBrandingLocalizedError(ErrCodeBrandingFaviconDeleteFailed))
		})

		It("tolerates glob and remove errors while removing stale branding assets", func() {
			Expect(func() {
				removeOtherMatches("[", "")
			}).NotTo(Panic())
			dir := tempBrandingDir()
			keep := filepath.Join(dir, "logo.png")
			stale := filepath.Join(dir, "logo.webp")
			Expect(os.WriteFile(keep, []byte("keep"), 0o600)).To(Succeed())
			Expect(os.MkdirAll(filepath.Join(stale, "child"), 0o755)).To(Succeed())

			removeOtherMatches(filepath.Join(dir, "logo.*"), keep)

			Expect(stale).To(BeADirectory())
		})
	})
})

func blockBrandingConfigPath(dir string) {
	GinkgoHelper()
	Expect(os.Mkdir(filepath.Join(dir, "branding.json"), 0o755)).To(Succeed())
}

func brandingTempUpload(contents []byte, pattern string) *os.File {
	GinkgoHelper()
	file, err := os.CreateTemp(tempBrandingDir(), pattern)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(closeTempBrandingFile, file)
	_, err = file.Write(contents)
	Expect(err).NotTo(HaveOccurred())
	_, err = file.Seek(0, 0)
	Expect(err).NotTo(HaveOccurred())
	return file
}

func tempBrandingDir() string {
	GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-branding-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, dir)
	return dir
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

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
	t := GinkgoT()
	svc, dir := newTestBrandingService(t)
	exactName := strings.Repeat("x", 100)

	if err := svc.UpdateBranding("  " + exactName + "  "); err != nil {
		t.Fatalf("UpdateBranding() error: %v", err)
	}

	cfg, err := NewBrandingStore(dir).Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.SiteName != exactName {
		t.Fatalf("SiteName = %q, want trimmed max-length name", cfg.SiteName)
	}
})

var _ = It("UpdateBranding allows common text whitespace in site names", func() {
	t := GinkgoT()
	svc, dir := newTestBrandingService(t)
	validName := "LeafWiki\tDocs\nTeam\rEdition"

	if err := svc.UpdateBranding(validName); err != nil {
		t.Fatalf("UpdateBranding() error: %v", err)
	}

	cfg, err := NewBrandingStore(dir).Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.SiteName != validName {
		t.Fatalf("SiteName = %q, want %q", cfg.SiteName, validName)
	}
})

var _ = It("UploadLogo accepts uppercase extensions and stores the normalized logo filename", func() {
	t := GinkgoT()
	svc, dir := newTestBrandingService(t)

	tmp, err := os.CreateTemp(t.TempDir(), "logo-*.PNG")
	if err != nil {
		t.Fatalf("CreateTemp() error: %v", err)
	}
	defer func() {
		if err := tmp.Close(); err != nil {
			t.Fatalf("Close() error: %v", err)
		}
	}()
	if _, err := tmp.Write([]byte("logo")); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		t.Fatalf("Seek() error: %v", err)
	}

	got, err := svc.UploadLogo(tmp, "CUSTOM.PNG")
	if err != nil {
		t.Fatalf("UploadLogo() error: %v", err)
	}
	if got != "logo.png" {
		t.Fatalf("UploadLogo() = %q, want logo.png", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "branding", "logo.png")); err != nil {
		t.Fatalf("expected normalized logo.png to exist: %v", err)
	}
})

var _ = It("UploadFavicon accepts a file exactly at the configured size limit", func() {
	t := GinkgoT()
	svc, dir := newTestBrandingService(t)
	svc.brandingConfig.BrandingConstraints.MaxFaviconSize = 10

	tmp, err := os.CreateTemp(t.TempDir(), "favicon-*.ico")
	if err != nil {
		t.Fatalf("CreateTemp() error: %v", err)
	}
	defer func() {
		if err := tmp.Close(); err != nil {
			t.Fatalf("Close() error: %v", err)
		}
	}()
	if _, err := tmp.Write(bytes.Repeat([]byte("f"), 10)); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
	if _, err := tmp.Seek(0, 0); err != nil {
		t.Fatalf("Seek() error: %v", err)
	}

	got, err := svc.UploadFavicon(tmp, "favicon.ico")
	if err != nil {
		t.Fatalf("UploadFavicon() error: %v", err)
	}
	if got != "favicon.ico" {
		t.Fatalf("UploadFavicon() = %q, want favicon.ico", got)
	}

	cfg, err := NewBrandingStore(dir).Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.FaviconFile != "favicon.ico" {
		t.Fatalf("FaviconFile = %q, want favicon.ico", cfg.FaviconFile)
	}
})

var _ = It("GetBrandingAssetsDir returns the package branding assets directory", func() {
	t := GinkgoT()
	svc, dir := newTestBrandingService(t)

	got := svc.GetBrandingAssetsDir()
	want := filepath.Join(dir, "branding")
	if got != want {
		t.Fatalf("GetBrandingAssetsDir() = %q, want %q", got, want)
	}
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
	t := GinkgoT()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "branding.json"), []byte("{broken json"), 0644); err != nil {
		t.Fatalf("WriteFile() error: %v", err)
	}

	_, err := NewBrandingService(dir)
	if err == nil {
		t.Fatal("expected NewBrandingService error for invalid persisted branding config")
	}
	if !strings.Contains(err.Error(), "failed to load branding config") {
		t.Fatalf("NewBrandingService error = %q, want load wrapper", err.Error())
	}
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

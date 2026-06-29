package branding

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
)

var _ = It("TestBrandingStore_Load_WhenConfigMissing_ReturnsDefault", func() {
	dir := GinkgoT().TempDir()
	store := NewBrandingStore(dir)

	cfg, err := store.Load()
	Expect(err).NotTo(HaveOccurred())

	def := DefaultBrandingConfig()

	Expect(cfg.SiteName).To(Equal(def.SiteName))
	Expect(cfg.LogoFile).To(Equal(def.LogoFile))
	Expect(cfg.FaviconFile).To(Equal(def.FaviconFile))

	// Constraints should be present (runtime-only)
	Expect(cfg.BrandingConstraints.MaxLogoSize).To(Equal(def.BrandingConstraints.MaxLogoSize))
	Expect(cfg.BrandingConstraints.MaxFaviconSize).To(Equal(def.BrandingConstraints.MaxFaviconSize))
	Expect(cfg.BrandingConstraints.LogoExts).NotTo(BeEmpty())
	Expect(cfg.BrandingConstraints.FaviconExts).NotTo(BeEmpty())
})

var _ = It("TestBrandingStore_SaveThenLoad_RoundTrip_PersistsFields", func() {
	dir := GinkgoT().TempDir()
	store := NewBrandingStore(dir)

	// Prepare config to save.
	cfg := DefaultBrandingConfig()
	cfg.SiteName = "MyWiki"
	cfg.LogoFile = "logo.png"
	cfg.FaviconFile = "favicon.ico"

	Expect(store.Save(cfg)).To(Succeed())

	got, err := store.Load()
	Expect(err).NotTo(HaveOccurred())

	Expect(got.SiteName).To(Equal("MyWiki"))
	Expect(got.LogoFile).To(Equal("logo.png"))
	Expect(got.FaviconFile).To(Equal("favicon.ico"))

	// Runtime-only constraints should be injected on Load, even though they are not persisted.
	def := DefaultBrandingConfig()
	Expect(got.BrandingConstraints.MaxLogoSize).To(Equal(def.BrandingConstraints.MaxLogoSize))
	Expect(got.BrandingConstraints.MaxFaviconSize).To(Equal(def.BrandingConstraints.MaxFaviconSize))
})

var _ = It("TestBrandingStore_Save_WritesFileToExpectedLocation", func() {
	dir := GinkgoT().TempDir()
	store := NewBrandingStore(dir)

	cfg := DefaultBrandingConfig()
	cfg.SiteName = "CheckFile"

	Expect(store.Save(cfg)).To(Succeed())

	p := filepath.Join(dir, "branding.json")
	info, err := os.Stat(p)
	Expect(err).NotTo(HaveOccurred())
	Expect(info.IsDir()).To(BeFalse())

	// Basic sanity: file contains our siteName
	b, err := os.ReadFile(p)
	Expect(err).NotTo(HaveOccurred())
	Expect(string(b)).To(ContainSubstring(`"siteName": "CheckFile"`))
})

var _ = It("TestBrandingStore_Load_WhenInvalidJSON_ReturnsError", func() {
	dir := GinkgoT().TempDir()
	store := NewBrandingStore(dir)

	// Write broken JSON
	Expect(os.WriteFile(filepath.Join(dir, "branding.json"), []byte("{not valid json"), 0644)).To(Succeed())

	_, err := store.Load()
	Expect(err).To(MatchError(ContainSubstring("failed to parse branding config")))
})

var _ = It("TestBrandingStore_Load_InsertsConstraintsEvenIfZeroInFile", func() {
	dir := GinkgoT().TempDir()
	store := NewBrandingStore(dir)

	// Save JSON that includes only persisted fields. BrandingConstraints is json:"-" and should be injected.
	raw := `{
  "siteName": "X",
  "logoFile": "logo.webp",
  "faviconFile": "favicon.png"
}`
	Expect(os.WriteFile(filepath.Join(dir, "branding.json"), []byte(raw), 0644)).To(Succeed())

	got, err := store.Load()
	Expect(err).NotTo(HaveOccurred())

	def := DefaultBrandingConfig()

	// Ensure constraints are injected and usable
	Expect(got.BrandingConstraints.MaxLogoSize).To(Equal(def.BrandingConstraints.MaxLogoSize))
	Expect(got.BrandingConstraints.LogoExts).To(HaveKeyWithValue(".png", def.BrandingConstraints.LogoExts[".png"]))
})

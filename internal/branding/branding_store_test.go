package branding

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"os"
	"path/filepath"
)

func matchBrandingConfig(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

func matchBrandingConstraints(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

var _ = It("TestBrandingStore_Load_WhenConfigMissing_ReturnsDefault", func() {
	dir := GinkgoT().TempDir()
	store := NewBrandingStore(dir)

	cfg, err := store.Load()
	Expect(err).NotTo(HaveOccurred())

	def := DefaultBrandingConfig()

	Expect(cfg).To(matchBrandingConfig(gstruct.Fields{
		"SiteName":    Equal(def.SiteName),
		"LogoFile":    Equal(def.LogoFile),
		"FaviconFile": Equal(def.FaviconFile),
		"BrandingConstraints": matchBrandingConstraints(gstruct.Fields{
			"MaxLogoSize":    Equal(def.BrandingConstraints.MaxLogoSize),
			"MaxFaviconSize": Equal(def.BrandingConstraints.MaxFaviconSize),
			"LogoExts":       Not(BeEmpty()),
			"FaviconExts":    Not(BeEmpty()),
		}),
	}))
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

	def := DefaultBrandingConfig()
	Expect(got).To(matchBrandingConfig(gstruct.Fields{
		"SiteName":    Equal("MyWiki"),
		"LogoFile":    Equal("logo.png"),
		"FaviconFile": Equal("favicon.ico"),
		"BrandingConstraints": matchBrandingConstraints(gstruct.Fields{
			"MaxLogoSize":    Equal(def.BrandingConstraints.MaxLogoSize),
			"MaxFaviconSize": Equal(def.BrandingConstraints.MaxFaviconSize),
		}),
	}))
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
	Expect(err).To(Satisfy(wrapsJSONSyntaxError))
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

	Expect(got).To(matchBrandingConfig(gstruct.Fields{
		"BrandingConstraints": matchBrandingConstraints(gstruct.Fields{
			"MaxLogoSize": Equal(def.BrandingConstraints.MaxLogoSize),
			"LogoExts":    HaveKeyWithValue(".png", def.BrandingConstraints.LogoExts[".png"]),
		}),
	}))
})

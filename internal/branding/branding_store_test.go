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

var _ = Describe("branding store", Label("integration"), func() {
	It("loads the default branding config when no persisted config exists", func() {
		dir := tempBrandingDir()
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

	It("persists branding fields across a save and load cycle", func() {
		dir := tempBrandingDir()
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

	It("writes the branding config file to the storage directory", func() {
		dir := tempBrandingDir()
		store := NewBrandingStore(dir)

		cfg := DefaultBrandingConfig()
		cfg.SiteName = "CheckFile"

		Expect(store.Save(cfg)).To(Succeed())

		p := filepath.Join(dir, "branding.json")
		Expect(p).To(BeAnExistingFile())

		// Basic sanity: file contains our siteName
		b, err := os.ReadFile(p)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(b)).To(ContainSubstring(`"siteName": "CheckFile"`))
	})

	It("returns a JSON syntax error when the persisted config is invalid", func() {
		dir := tempBrandingDir()
		store := NewBrandingStore(dir)

		// Write broken JSON
		Expect(os.WriteFile(filepath.Join(dir, "branding.json"), []byte("{not valid json"), 0644)).To(Succeed())

		_, err := store.Load()
		Expect(err).To(Satisfy(wrapsJSONSyntaxError))
	})

	It("injects runtime constraints when persisted config omits them", func() {
		dir := tempBrandingDir()
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
})

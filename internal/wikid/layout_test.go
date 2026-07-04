package wikid

import (
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = ginkgo.Describe("global wikid layout", ginkgo.Label("unit"), func() {
	ginkgo.It("derives registry, runtime, and home workspace paths under the LeafWiki home directory", func() {
		homeDir := filepath.Join(wikidTestTempDir(), ".leafwiki")

		layout := GlobalLayout(homeDir)

		Expect(layout).To(SatisfyAll(
			HaveField("HomeDir", Equal(homeDir)),
			HaveField("WikidDir", Equal(filepath.Join(homeDir, "wikid"))),
			HaveField("RuntimeDir", Equal(filepath.Join(homeDir, "runtime"))),
			HaveField("DBPath", Equal(filepath.Join(homeDir, "wikid", "wikid.db"))),
			HaveField("HomeRootDir", Equal(filepath.Join(homeDir, "root"))),
		))
	})
})

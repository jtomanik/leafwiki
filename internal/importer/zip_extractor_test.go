package importer

import (
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func removeExtractedZipWorkspace(ws *ZipWorkspace) {
	ginkgo.GinkgoHelper()

	Expect(ws.Cleanup()).To(Succeed())
}

var _ = ginkgo.Describe("zip workspace extraction", ginkgo.Label("unit"), func() {
	ginkgo.It("extracts the fixture archive with expected markdown files", func() {
		currentDir, err := os.Getwd()
		Expect(err).To(Succeed())
		zipPath := "fixtures/fixture-1.zip"

		extractor := NewZipExtractor()
		ws, err := extractor.ExtractToTemp(filepath.Join(currentDir, zipPath))
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(removeExtractedZipWorkspace, ws)

		// Check if expected files exist
		expectedFiles := []string{
			"home.md",
			"features/index.md",
			"features/mermaind.md",
		}

		for _, relPath := range expectedFiles {
			fullPath := filepath.Join(ws.Root, relPath)
			_, err := os.Stat(fullPath)
			Expect(err).To(Succeed())
		}

	})
})

var _ = ginkgo.Describe("zip workspace cleanup", ginkgo.Label("unit"), func() {
	ginkgo.It("removes the extracted workspace root", func() {
		currentDir, err := os.Getwd()
		Expect(err).To(Succeed())
		zipPath := "fixtures/fixture-1.zip"

		extractor := NewZipExtractor()
		ws, err := extractor.ExtractToTemp(filepath.Join(currentDir, zipPath))
		Expect(err).To(Succeed())

		workspaceRoot := ws.Root

		// Cleanup
		removeExtractedZipWorkspace(ws)

		// Verify cleanup
		_, err = os.Stat(workspaceRoot)
		Expect(err).To(MatchError(os.ErrNotExist))

	})
})

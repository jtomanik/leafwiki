package revision

import (
	"os"
	"path/filepath"

	ginkgo "github.com/onsi/ginkgo/v2"

	. "github.com/onsi/gomega"
	"github.com/perber/wiki/internal/core/markdown"
	"github.com/perber/wiki/internal/core/tree"
)

func newRevisionTestService() (*Service, *tree.TreeService, string) {
	ginkgo.GinkgoHelper()

	storageDir := revisionTempDir()
	treeService := tree.NewTreeService(storageDir)
	Expect(treeService.LoadTree()).To(Succeed())

	return NewService(storageDir, treeService, nil), treeService, storageDir
}

func createRevisionTestPage(treeService *tree.TreeService, title, slug, content string) tree.PageID {
	ginkgo.GinkgoHelper()

	kind := tree.NodeKindPage
	id, err := treeService.CreateNode("tester", nil, title, tree.SlugFromString(slug), &kind)
	Expect(err).NotTo(HaveOccurred())
	Expect(id).NotTo(BeNil())
	Expect(treeService.UpdateNodeUncheckedVersion(newFixtureUserID("tester"), *id, title, tree.SlugFromString(slug), &content, false)).To(Succeed())
	return *id
}

func revisionTestPageID[T ~string](raw T) tree.PageID {
	return tree.PageIDFromString(raw)
}

func revisionTestUserID(raw string) tree.UserID {
	return tree.UserIDFromString(raw)
}

func renderRevisionTestMarkdown[T ~string](pageID T, title string, fields map[string]interface{}, extra map[string]interface{}, body string) string {
	ginkgo.GinkgoHelper()

	raw, err := markdown.RenderPageDocument(markdown.PageDocument{
		Body: body,
		Metadata: markdown.PageMetadata{
			Version: 1,
			Page: markdown.PageMetadataPage{
				ID:    string(pageID),
				Title: title,
			},
			Fields: fields,
			Extra:  extra,
		},
	})
	Expect(err).NotTo(HaveOccurred())
	return raw
}

func revisionAssetPath[T ~string](storageDir string, pageID T, parts ...string) string {
	elems := append([]string{storageDir, "assets", string(pageID)}, parts...)
	return filepath.Join(elems...)
}

func writeLiveAsset[T ~string](storageDir string, pageID T, name, content string) {
	ginkgo.GinkgoHelper()

	dir := revisionAssetPath(storageDir, pageID)
	Expect(os.MkdirAll(dir, 0o755)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644)).To(Succeed())
}

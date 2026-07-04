package gitrevisions

import (
	"os"
	"path/filepath"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func actorIDStrings(ids []ActorID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}

type managedMarkdownPathClass string

const (
	managedMarkdownRevisionPath          managedMarkdownPathClass = "managed markdown revision path"
	ignoredHiddenDirectoryMarkdownPath   managedMarkdownPathClass = "ignored hidden-directory markdown path"
	ignoredHiddenFilenameMarkdownPath    managedMarkdownPathClass = "ignored hidden-filename markdown path"
	ignoredSwapMarkdownPath              managedMarkdownPathClass = "ignored markdown swap path"
	ignoredNonMarkdownRevisionPath       managedMarkdownPathClass = "ignored non-markdown revision path"
	ignoredUnexpectedManagedPathDecision managedMarkdownPathClass = "unexpected managed markdown path decision"
)

func managedMarkdownPathClassFor(relPath string) managedMarkdownPathClass {
	if IsManagedMarkdownRelPath(relPath) {
		return managedMarkdownRevisionPath
	}

	slashPath := filepath.ToSlash(relPath)
	for _, segment := range strings.Split(slashPath, "/") {
		if strings.HasPrefix(segment, ".") {
			if strings.EqualFold(filepath.Ext(segment), ".md") {
				return ignoredHiddenFilenameMarkdownPath
			}
			return ignoredHiddenDirectoryMarkdownPath
		}
	}

	base := filepath.Base(slashPath)
	if strings.EqualFold(filepath.Ext(base), ".swp") && strings.EqualFold(filepath.Ext(strings.TrimSuffix(base, filepath.Ext(base))), ".md") {
		return ignoredSwapMarkdownPath
	}
	if !strings.EqualFold(filepath.Ext(base), ".md") {
		return ignoredNonMarkdownRevisionPath
	}
	return ignoredUnexpectedManagedPathDecision
}

func matchManagedMarkdownPathClass(expected managedMarkdownPathClass) OmegaMatcher {
	return WithTransform(managedMarkdownPathClassFor, Equal(expected))
}

func gitRevisionTempDir() string {
	GinkgoHelper()

	dir, err := os.MkdirTemp("", "leafwiki-gitrevisions-*")
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(os.RemoveAll, dir)
	return dir
}

func writeFile(path string, content string) {
	GinkgoHelper()

	Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
	Expect(os.WriteFile(path, []byte(content), 0o644)).To(Succeed())
}

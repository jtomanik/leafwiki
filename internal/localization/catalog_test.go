package localization

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("committed localization catalog", func() {
	It("TestCommittedCatalogCoversProductionMessageIDConstants", func() {
		t := GinkgoT()
		repoRoot := repoRoot(t)
		catalog, err := committedCatalog()
		Expect(err).NotTo(HaveOccurred())
		missing := []string{}
		for _, id := range productionMessageIDConstants(t, repoRoot) {
			if _, ok := catalog[id]; !ok {
				missing = append(missing, id)
			}
		}
		Expect(missing).To(BeEmpty(), "catalog missing production MessageID constants: %s", strings.Join(missing, ", "))
	})

	It("TestProductionMessageIDConstantsIncludesMCPToolMessageIDs", func() {
		t := GinkgoT()
		ids := map[string]struct{}{}
		for _, id := range productionMessageIDConstants(t, repoRoot(t)) {
			ids[id] = struct{}{}
		}
		Expect(ids).To(HaveKey("mcp.tools.wiki_move_page.success"))
	})

	It("TestProductionMessageIDConstantsIncludesMCPToolDescriptionIDs", func() {
		t := GinkgoT()
		ids := map[string]struct{}{}
		for _, id := range productionMessageIDConstants(t, repoRoot(t)) {
			ids[id] = struct{}{}
		}
		Expect(ids).To(HaveKey("mcp.tools.wiki_move_page.description"))
	})

	It("TestCommittedCatalogCoversProductionErrorCodeMessageIDs", func() {
		t := GinkgoT()
		repoRoot := repoRoot(t)
		catalog, err := committedCatalog()
		Expect(err).NotTo(HaveOccurred())
		missing := []string{}
		for _, id := range productionErrorCodeMessageIDs(t, repoRoot) {
			if _, ok := catalog[id]; !ok {
				missing = append(missing, id)
			}
		}
		Expect(missing).To(BeEmpty(), "catalog missing derived ErrorCode message IDs: %s", strings.Join(missing, ", "))
	})
})

type catalogTestTB interface {
	Helper()
	Fatalf(format string, args ...any)
}

func productionMessageIDConstants(t catalogTestTB, repoRoot string) []string {
	t.Helper()
	re := regexp.MustCompile(`(?m)(?:MessageID|ToolMessage|ToolDescription)[A-Za-z0-9_]*\s+[^=\n]*=\s+"([^"]+)"`)
	ids := map[string]struct{}{}
	err := filepath.WalkDir(filepath.Join(repoRoot, "internal"), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if name == "analysis" || name == "localization" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range re.FindAllStringSubmatch(string(raw), -1) {
			ids[match[1]] = struct{}{}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk production Go files: %v", err)
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	return out
}

func productionErrorCodeMessageIDs(t catalogTestTB, repoRoot string) []string {
	t.Helper()
	re := regexp.MustCompile(`(?m)(?:ErrCode|errCode|runtimeErrorCode)[A-Za-z0-9_]*\s+sharederrors\.ErrorCode\s*=\s*"([^"]+)"`)
	ids := map[string]struct{}{}
	for _, root := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, root), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				name := d.Name()
				if name == "analysis" || name == "localization" {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, match := range re.FindAllStringSubmatch(string(raw), -1) {
				ids[messageIDForErrorCode(match[1])] = struct{}{}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s Go files: %v", root, err)
		}
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	return out
}

func messageIDForErrorCode(code string) string {
	head, tail, ok := strings.Cut(strings.TrimSpace(code), "_")
	if !ok || strings.TrimSpace(tail) == "" {
		return "errors." + code
	}
	return "errors." + head + "." + tail
}

func repoRoot(t catalogTestTB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(dir, "..", ".."))
}

package localization

import (
	"embed"
	"io/fs"
	"sync"
	"testing/fstest"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("English renderer", func() {
	It("TestEnglishRendererUsesCatalogAndPositionalArgs", func() {
		rendered := English.Render("errors.page.version_conflict", "fallback", "docs.md", "README.md")

		Expect(rendered.Message).To(ContainSubstring("docs.md"))
		Expect(rendered.Message).To(ContainSubstring("README.md"))
		Expect(rendered.Missing).To(BeFalse())
		Expect(rendered.Err).NotTo(HaveOccurred())
	})

	It("TestEnglishRendererFallsBackWhenCatalogEntryIsMissing", func() {
		rendered := English.Render("errors.test.missing", "Default fallback {{.Arg0}}", "value")

		Expect(rendered.Message).To(Equal("Default fallback value"))
		Expect(rendered.Missing).To(BeTrue())
	})

	It("TestEnglishRendererFallsBackWhenTemplateDataDoesNotMatch", func() {
		rendered := English.Render("errors.page.version_conflict", "safe fallback {{.Arg0}}", "page.md")

		Expect(rendered.Message).To(Equal("safe fallback page.md"))
		Expect(rendered.Err).To(HaveOccurred())
	})

	It("TestRegistryRejectsDuplicateMessageIDWithDifferentEnglish", func() {
		err := validateDefinitions([]Definition{
			{ID: "cli.help.usage", Default: "Usage: leafwiki [command]"},
			{ID: "cli.help.usage", Default: "Usage: other"},
		})

		Expect(err).To(MatchError(ContainSubstring("conflicting defaults")))
	})

	It("TestCommittedCatalogCoversRegistry", func() {
		Expect(ValidateCommittedCatalog()).To(Succeed())
	})

	It("TestEnglishRendererIsConcurrentSafe", func() {
		var wg sync.WaitGroup
		errs := make(chan string, 32)
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rendered := English.Render("cli.help.usage", "fallback")
				if rendered.Message == "" || rendered.Missing || rendered.Err != nil {
					errs <- rendered.Message
				}
			}()
		}
		wg.Wait()
		close(errs)

		Expect(errs).To(BeEmpty())
	})
})

type stringMessageID string

func (id stringMessageID) String() string {
	return string(id)
}

var _ = Describe("localization edge coverage", func() {
	It("nil renderer falls back using positional template data", func() {
		var renderer *Renderer

		rendered := renderer.Render("cli.help.usage", "fallback {{.Arg0}}", "value")

		Expect(rendered.Message).To(Equal("fallback value"))
		Expect(rendered.Missing).To(BeFalse())
		Expect(rendered.Err).NotTo(HaveOccurred())
	})

	It("nil renderer reports missing catalog IDs", func() {
		var renderer *Renderer

		Expect(renderer.hasCatalogID("cli.help.usage")).To(BeFalse())
	})

	It("empty message IDs use the fallback message", func() {
		rendered := English.Render("", "fallback {{.Arg0}}", "value")

		Expect(rendered.Message).To(Equal("fallback value"))
		Expect(rendered.Missing).To(BeFalse())
		Expect(rendered.Err).NotTo(HaveOccurred())
	})

	It("message IDs implementing String render through catalog lookup", func() {
		rendered := English.Render(stringMessageID("cli.help.usage"), "fallback")

		Expect(rendered.Message).To(ContainSubstring("Usage: leafwiki"))
		Expect(rendered.Missing).To(BeFalse())
		Expect(rendered.Err).NotTo(HaveOccurred())
	})

	It("non-string message IDs are stringified before fallback rendering", func() {
		rendered := English.Render(123, "fallback {{.Arg0}}", "value")

		Expect(rendered.Message).To(Equal("fallback value"))
		Expect(rendered.Missing).To(BeTrue())
		Expect(rendered.Err).To(HaveOccurred())
	})

	It("fallback rendering returns literal default when template is invalid", func() {
		rendered := English.Render("errors.test.missing", "Default {{")

		Expect(rendered.Message).To(Equal("Default {{"))
		Expect(rendered.Missing).To(BeTrue())
	})

	It("fallback rendering returns literal default when arguments are missing", func() {
		rendered := English.Render("errors.test.missing", "Default {{.Arg1}}", "value")

		Expect(rendered.Message).To(Equal("Default {{.Arg1}}"))
		Expect(rendered.Missing).To(BeTrue())
	})

	It("validateDefinitions rejects empty IDs", func() {
		err := validateDefinitions([]Definition{{ID: " ", Default: "Default"}})

		Expect(err).To(MatchError(ContainSubstring("empty ID")))
	})

	It("validateDefinitions rejects empty defaults", func() {
		err := validateDefinitions([]Definition{{ID: "cli.test", Default: " "}})

		Expect(err).To(MatchError(ContainSubstring("empty default")))
	})

	It("validateDefinitions accepts duplicate IDs with identical defaults", func() {
		err := validateDefinitions([]Definition{
			{ID: "cli.test", Default: "Same"},
			{ID: "cli.test", Default: "Same"},
		})

		Expect(err).NotTo(HaveOccurred())
	})

	It("NewEnglishRenderer returns a renderer that renders known catalog IDs", func() {
		renderer, err := NewEnglishRenderer()
		Expect(err).NotTo(HaveOccurred())

		rendered := renderer.Render("cli.help.usage", "fallback")

		Expect(rendered.Message).To(ContainSubstring("Usage: leafwiki"))
		Expect(rendered.Missing).To(BeFalse())
		Expect(rendered.Err).NotTo(HaveOccurred())
	})

	It("Definitions include derived error and shell run messages", func() {
		definitions := Definitions()
		ids := map[string]struct{}{}
		for _, definition := range definitions {
			ids[definition.ID] = struct{}{}
		}

		Expect(ids).To(HaveKey("errors.workspace.id_required"))
		Expect(ids).To(HaveKey(MessageIDShellRunUsage))
		Expect(ids).To(HaveKey(MessageIDShellRunErrorUnknownOption))
	})

	It("Definitions ignores nil registry messages", func() {
		before := Definitions()
		previous := registryMessages
		registryMessages = append(registryMessages, nil)
		DeferCleanup(func() {
			registryMessages = previous
		})

		Expect(Definitions()).To(HaveLen(len(before)))
	})

	It("renderer construction and committed catalog validation reject invalid registry definitions", func() {
		replaceRegistryMessages([]*i18n.Message{{ID: " ", Other: "Default"}})

		Expect(ValidateCommittedCatalog()).To(MatchError(ContainSubstring("empty ID")))
		renderer, err := NewEnglishRenderer()
		Expect(err).To(MatchError(ContainSubstring("empty ID")))
		Expect(renderer).To(BeNil())
		Expect(func() {
			mustNewEnglishRenderer()
		}).To(Panic())
	})

	It("ValidateCommittedCatalog reports registry messages missing from the committed catalog", func() {
		messages := append([]*i18n.Message(nil), registryMessages...)
		messages = append(messages, &i18n.Message{
			ID:          "test.missing.catalog",
			Description: "Synthetic test message.",
			Other:       "Synthetic test message",
		})
		replaceRegistryMessages(messages)

		err := ValidateCommittedCatalog()

		Expect(err).To(MatchError(ContainSubstring("catalog missing message IDs: test.missing.catalog")))
	})

	It("ValidateCommittedCatalog reports committed catalog default mismatches", func() {
		messages := append([]*i18n.Message(nil), registryMessages...)
		for i, message := range messages {
			if message != nil && message.ID == MessageIDCLIHelpUsage {
				clone := *message
				clone.Other = "Different usage"
				messages[i] = &clone
				break
			}
		}
		replaceRegistryMessages(messages)

		err := ValidateCommittedCatalog()

		Expect(err).To(MatchError(ContainSubstring("catalog cli.help.usage other")))
	})

	It("catalog readers return errors when the embedded catalog is unavailable", func() {
		replaceLocaleFS(embed.FS{})

		catalog, err := committedCatalog()
		Expect(err).To(MatchError(ContainSubstring("read English catalog")))
		Expect(catalog).To(BeNil())

		ids, err := catalogIDsFromCommittedCatalog()
		Expect(err).To(MatchError(ContainSubstring("read English catalog")))
		Expect(ids).To(BeNil())

		Expect(ValidateCommittedCatalog()).To(MatchError(ContainSubstring("read English catalog")))
		renderer, err := NewEnglishRenderer()
		Expect(err).To(MatchError(ContainSubstring("read English catalog")))
		Expect(renderer).To(BeNil())
	})

	It("catalog readers report invalid TOML in the embedded catalog", func() {
		replaceLocaleFS(fstest.MapFS{
			"locales/active.en.toml": &fstest.MapFile{Data: []byte("[")},
		})

		catalog, err := committedCatalog()

		Expect(err).To(MatchError(ContainSubstring("parse English catalog")))
		Expect(catalog).To(BeNil())
	})

	It("renderer construction reports loader errors after catalog IDs are read", func() {
		replaceLocaleFS(&sequentialCatalogFS{
			files: []fstest.MapFS{
				{
					"locales/active.en.toml": &fstest.MapFile{Data: []byte(`["cli.help.usage"]
description = "Usage."
other = "Usage: leafwiki [command]"
`)},
				},
				{
					"locales/active.en.toml": &fstest.MapFile{Data: []byte("[")},
				},
			},
		})

		renderer, err := NewEnglishRenderer()

		Expect(err).To(MatchError(ContainSubstring("load English catalog")))
		Expect(renderer).To(BeNil())
	})
})

func replaceRegistryMessages(messages []*i18n.Message) {
	GinkgoHelper()
	previous := registryMessages
	registryMessages = messages
	DeferCleanup(func() {
		registryMessages = previous
	})
}

func replaceLocaleFS(fsys fs.FS) {
	GinkgoHelper()
	previous := localeFS
	localeFS = fsys
	DeferCleanup(func() {
		localeFS = previous
	})
}

type sequentialCatalogFS struct {
	files []fstest.MapFS
	opens int
}

func (fsys *sequentialCatalogFS) Open(name string) (fs.File, error) {
	index := fsys.opens
	if index >= len(fsys.files) {
		index = len(fsys.files) - 1
	}
	fsys.opens++
	return fsys.files[index].Open(name)
}

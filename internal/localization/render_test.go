package localization

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"testing/fstest"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
)

const (
	missingCatalogFallbackTemplate     = "Default fallback {{.Arg0}}"
	missingCatalogFallbackRendered     = "Default fallback value"
	safeFallbackTemplate               = "safe fallback {{.Arg0}}"
	safeFallbackRendered               = "safe fallback page.md"
	genericFallbackTemplate            = "fallback {{.Arg0}}"
	genericFallbackRendered            = "fallback value"
	invalidFallbackTemplate            = "Default {{"
	missingArgumentFallbackTemplate    = "Default {{.Arg1}}"
	duplicateMessageDefaultUsage       = "Usage: leafwiki [command]"
	duplicateMessageDefaultOther       = "Usage: other"
	registryDefaultValue               = "Default"
	testCatalogMissingID               = "test.missing.catalog"
	testCatalogMissingDefault          = "Synthetic test message"
	testCatalogMismatchDefault         = "Different usage"
	testCatalogFixtureUsageDescription = "Usage."
	testCatalogFixtureUsageDefault     = "Usage: leafwiki [command]"
)

var _ = Describe("English renderer", func() {
	It("renders catalog messages with positional arguments", func() {
		rendered := English.Render("errors.page.version_conflict", "fallback", "docs.md", "README.md")

		Expect(rendered).To(matchRenderResult(gstruct.Fields{
			"Message": SatisfyAll(
				ContainSubstring("docs.md"),
				ContainSubstring("README.md"),
			),
			"Missing": BeFalse(),
			"Err":     Not(HaveOccurred()),
		}))
	})

	It("renders the fallback template and marks missing catalog entries", func() {
		rendered := English.Render("errors.test.missing", missingCatalogFallbackTemplate, "value")

		Expect(rendered).To(matchRenderResult(gstruct.Fields{
			"Message": Equal(missingCatalogFallbackRendered),
			"Missing": BeTrue(),
		}))
	})

	It("uses the fallback template when catalog data cannot render safely", func() {
		rendered := English.Render(messageIDPageVersionConflict, safeFallbackTemplate, "page.md")

		Expect(rendered).To(matchRenderResult(gstruct.Fields{
			"Message": Equal(safeFallbackRendered),
			"Missing": BeFalse(),
			"Err":     matchTemplateDataMismatch(messageIDPageVersionConflict),
		}))
	})

	It("rejects duplicate message IDs with conflicting English defaults", func() {
		err := validateDefinitions([]Definition{
			{ID: MessageIDCLIHelpUsage, Default: duplicateMessageDefaultUsage},
			{ID: MessageIDCLIHelpUsage, Default: duplicateMessageDefaultOther},
		})

		Expect(err).To(MatchError(ErrMessageDefinitionDefaultConflict))
	})

	It("keeps the committed catalog synchronized with registry definitions", func() {
		Expect(ValidateCommittedCatalog()).To(Succeed())
	})

	It("renders English messages safely from concurrent callers", func() {
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

var _ = Describe("localization fallback and catalog validation contracts", func() {
	It("nil renderer falls back using positional template data", func() {
		var renderer *Renderer

		rendered := renderer.Render(MessageIDCLIHelpUsage, genericFallbackTemplate, "value")

		Expect(rendered).To(matchRenderResult(gstruct.Fields{
			"Message": Equal(genericFallbackRendered),
			"Missing": BeFalse(),
			"Err":     Not(HaveOccurred()),
		}))
	})

	It("nil renderer reports missing catalog IDs", func() {
		var renderer *Renderer

		Expect(renderer.hasCatalogID(MessageIDCLIHelpUsage)).To(BeFalse())
	})

	It("empty message IDs use the fallback message", func() {
		rendered := English.Render("", genericFallbackTemplate, "value")

		Expect(rendered).To(matchRenderResult(gstruct.Fields{
			"Message": Equal(genericFallbackRendered),
			"Missing": BeFalse(),
			"Err":     Not(HaveOccurred()),
		}))
	})

	It("message IDs implementing String render through catalog lookup", func() {
		rendered := English.Render(stringMessageID(MessageIDCLIHelpUsage), "fallback")

		Expect(rendered).To(matchRenderResult(gstruct.Fields{
			"Message": Not(Equal("fallback")),
			"Missing": BeFalse(),
			"Err":     Not(HaveOccurred()),
		}))
	})

	It("non-string message IDs are stringified before fallback rendering", func() {
		rendered := English.Render(123, genericFallbackTemplate, "value")

		Expect(rendered).To(matchRenderResult(gstruct.Fields{
			"Message": Equal(genericFallbackRendered),
			"Missing": BeTrue(),
			"Err":     matchMissingCatalogMessage(fmt.Sprint(123)),
		}))
	})

	It("fallback rendering returns literal default when template is invalid", func() {
		rendered := English.Render("errors.test.missing", invalidFallbackTemplate)

		Expect(rendered).To(matchRenderResult(gstruct.Fields{
			"Message": Equal(invalidFallbackTemplate),
			"Missing": BeTrue(),
		}))
	})

	It("fallback rendering returns literal default when arguments are missing", func() {
		rendered := English.Render("errors.test.missing", missingArgumentFallbackTemplate, "value")

		Expect(rendered).To(matchRenderResult(gstruct.Fields{
			"Message": Equal(missingArgumentFallbackTemplate),
			"Missing": BeTrue(),
		}))
	})

	It("rejects registry definitions without a message ID", func() {
		err := validateDefinitions([]Definition{{ID: " ", Default: registryDefaultValue}})

		Expect(err).To(MatchError(ErrMessageDefinitionIDRequired))
	})

	It("rejects registry definitions without an English default", func() {
		err := validateDefinitions([]Definition{{ID: "cli.test", Default: " "}})

		Expect(err).To(MatchError(ErrMessageDefinitionDefaultMissing))
	})

	It("allows repeated message IDs when their English defaults match", func() {
		err := validateDefinitions([]Definition{
			{ID: "cli.test", Default: "Same"},
			{ID: "cli.test", Default: "Same"},
		})

		Expect(err).NotTo(HaveOccurred())
	})

	It("NewEnglishRenderer returns a renderer that renders known catalog IDs", func() {
		renderer, err := NewEnglishRenderer()
		Expect(err).NotTo(HaveOccurred())

		rendered := renderer.Render(MessageIDCLIHelpUsage, "fallback")

		Expect(rendered).To(matchRenderResult(gstruct.Fields{
			"Message": Not(Equal("fallback")),
			"Missing": BeFalse(),
			"Err":     Not(HaveOccurred()),
		}))
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
		replaceRegistryMessages([]*i18n.Message{{ID: " ", Other: registryDefaultValue}})

		Expect(ValidateCommittedCatalog()).To(MatchError(ErrMessageDefinitionIDRequired))
		renderer, err := NewEnglishRenderer()
		Expect(err).To(MatchError(ErrMessageDefinitionIDRequired))
		Expect(renderer).To(BeNil())
		Expect(func() {
			mustNewEnglishRenderer()
		}).To(Panic())
	})

	It("ValidateCommittedCatalog reports registry messages missing from the committed catalog", func() {
		messages := append([]*i18n.Message(nil), registryMessages...)
		messages = append(messages, &i18n.Message{
			ID:          testCatalogMissingID,
			Description: "Synthetic test message.",
			Other:       testCatalogMissingDefault,
		})
		replaceRegistryMessages(messages)

		err := ValidateCommittedCatalog()

		Expect(err).To(matchCommittedCatalogMissingMessage(CatalogMessageID(testCatalogMissingID)))
	})

	It("ValidateCommittedCatalog reports committed catalog default mismatches", func() {
		messages := append([]*i18n.Message(nil), registryMessages...)
		for i, message := range messages {
			if message != nil && message.ID == MessageIDCLIHelpUsage {
				clone := *message
				clone.Other = testCatalogMismatchDefault
				messages[i] = &clone
				break
			}
		}
		replaceRegistryMessages(messages)

		err := ValidateCommittedCatalog()

		Expect(err).To(matchCommittedCatalogDefaultMismatch(CatalogMessageID(MessageIDCLIHelpUsage)))
	})

	It("catalog readers return errors when the embedded catalog is unavailable", func() {
		replaceLocaleFS(embed.FS{})

		catalog, err := committedCatalog()
		Expect(err).To(SatisfyAll(
			MatchError(ErrEnglishCatalogRead),
			MatchError(fs.ErrNotExist),
		))
		Expect(catalog).To(BeNil())

		ids, err := catalogIDsFromCommittedCatalog()
		Expect(err).To(SatisfyAll(
			MatchError(ErrEnglishCatalogRead),
			MatchError(fs.ErrNotExist),
		))
		Expect(ids).To(BeNil())

		Expect(ValidateCommittedCatalog()).To(SatisfyAll(
			MatchError(ErrEnglishCatalogRead),
			MatchError(fs.ErrNotExist),
		))
		renderer, err := NewEnglishRenderer()
		Expect(err).To(SatisfyAll(
			MatchError(ErrEnglishCatalogRead),
			MatchError(fs.ErrNotExist),
		))
		Expect(renderer).To(BeNil())
	})

	It("catalog readers report invalid TOML in the embedded catalog", func() {
		replaceLocaleFS(fstest.MapFS{
			"locales/active.en.toml": &fstest.MapFile{Data: []byte("[")},
		})

		catalog, err := committedCatalog()

		Expect(err).To(MatchError(ErrEnglishCatalogParse))
		Expect(catalog).To(BeNil())
	})

	It("renderer construction reports loader errors after catalog IDs are read", func() {
		replaceLocaleFS(&sequentialCatalogFS{
			files: []fstest.MapFS{
				{
					"locales/active.en.toml": &fstest.MapFile{Data: []byte(`["cli.help.usage"]
description = "` + testCatalogFixtureUsageDescription + `"
other = "` + testCatalogFixtureUsageDefault + `"
`)},
				},
				{
					"locales/active.en.toml": &fstest.MapFile{Data: []byte("[")},
				},
			},
		})

		renderer, err := NewEnglishRenderer()

		Expect(err).To(MatchError(ErrEnglishCatalogLoad))
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

func matchRenderResult(fields gstruct.Fields) types.GomegaMatcher {
	GinkgoHelper()
	return gstruct.MatchFields(gstruct.IgnoreExtras, fields)
}

func matchMissingCatalogMessage(id string) types.GomegaMatcher {
	GinkgoHelper()
	return WithTransform(func(err error) string {
		var missingErr *i18n.MessageNotFoundErr
		if !errors.As(err, &missingErr) {
			return ""
		}
		return missingErr.MessageID
	}, Equal(id))
}

func matchTemplateDataMismatch(id CatalogMessageID) types.GomegaMatcher {
	GinkgoHelper()
	return SatisfyAll(
		MatchError(ErrLocalizationTemplateDataMismatch),
		WithTransform(func(err error) CatalogMessageID {
			var mismatchErr *TemplateDataMismatchError
			if !errors.As(err, &mismatchErr) {
				return ""
			}
			return mismatchErr.MessageID
		}, Equal(id)),
	)
}

func matchCommittedCatalogMissingMessage(id CatalogMessageID) types.GomegaMatcher {
	GinkgoHelper()
	return SatisfyAll(
		MatchError(ErrCommittedCatalogMissingMessage),
		WithTransform(func(err error) []CatalogMessageID {
			var catalogErr *CommittedCatalogMissingMessagesError
			if !errors.As(err, &catalogErr) {
				return nil
			}
			return catalogErr.MessageIDs()
		}, ContainElement(id)),
	)
}

func matchCommittedCatalogDefaultMismatch(id CatalogMessageID) types.GomegaMatcher {
	GinkgoHelper()
	return SatisfyAll(
		MatchError(ErrCommittedCatalogDefaultMismatch),
		WithTransform(func(err error) CatalogMessageID {
			var catalogErr *CommittedCatalogDefaultMismatchError
			if !errors.As(err, &catalogErr) {
				return ""
			}
			return catalogErr.ID
		}, Equal(id)),
	)
}

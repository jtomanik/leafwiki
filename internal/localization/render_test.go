package localization

import (
	"strings"
	"sync"
	"testing"
)

func TestEnglishRendererUsesCatalogAndPositionalArgs(t *testing.T) {
	t.Parallel()

	rendered := English.Render("errors.page.version_conflict", "fallback", "docs.md", "README.md")

	if !strings.Contains(rendered.Message, "docs.md") || !strings.Contains(rendered.Message, "README.md") {
		t.Fatalf("rendered message = %q, want catalog message with positional args", rendered.Message)
	}
	if rendered.Missing {
		t.Fatalf("rendered.Missing = true, want false")
	}
	if rendered.Err != nil {
		t.Fatalf("rendered.Err = %v, want nil", rendered.Err)
	}
}

func TestEnglishRendererFallsBackWhenCatalogEntryIsMissing(t *testing.T) {
	t.Parallel()

	rendered := English.Render("errors.test.missing", "Default fallback {{.Arg0}}", "value")

	if rendered.Message != "Default fallback value" {
		t.Fatalf("rendered.Message = %q, want default fallback with args", rendered.Message)
	}
	if !rendered.Missing {
		t.Fatalf("rendered.Missing = false, want true")
	}
}

func TestEnglishRendererFallsBackWhenTemplateDataDoesNotMatch(t *testing.T) {
	t.Parallel()

	rendered := English.Render("errors.page.version_conflict", "safe fallback {{.Arg0}}", "page.md")

	if rendered.Message != "safe fallback page.md" {
		t.Fatalf("rendered.Message = %q, want safe fallback", rendered.Message)
	}
	if rendered.Err == nil {
		t.Fatalf("rendered.Err = nil, want template mismatch error")
	}
}

func TestRegistryRejectsDuplicateMessageIDWithDifferentEnglish(t *testing.T) {
	t.Parallel()

	err := validateDefinitions([]Definition{
		{ID: "cli.help.usage", Default: "Usage: leafwiki [command]"},
		{ID: "cli.help.usage", Default: "Usage: other"},
	})

	if err == nil {
		t.Fatalf("validateDefinitions returned nil, want duplicate ID error")
	}
}

func TestCommittedCatalogCoversRegistry(t *testing.T) {
	t.Parallel()

	if err := ValidateCommittedCatalog(); err != nil {
		t.Fatalf("ValidateCommittedCatalog: %v", err)
	}
}

func TestEnglishRendererIsConcurrentSafe(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rendered := English.Render("cli.help.usage", "fallback")
			if rendered.Message == "" || rendered.Missing || rendered.Err != nil {
				t.Errorf("rendered = %#v, want catalog-backed message", rendered)
			}
		}()
	}
	wg.Wait()
}

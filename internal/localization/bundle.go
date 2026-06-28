package localization

import (
	"embed"
	"fmt"
	"io/fs"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/active.en.toml
var embeddedLocaleFS embed.FS

var localeFS fs.FS = embeddedLocaleFS

var English = mustNewEnglishRenderer()

type Renderer struct {
	localizer       *i18n.Localizer
	catalogMessages map[string]struct{}
	mu              sync.RWMutex
}

func mustNewEnglishRenderer() *Renderer {
	renderer, err := NewEnglishRenderer()
	if err != nil {
		panic(err)
	}
	return renderer
}

func NewEnglishRenderer() (*Renderer, error) {
	if err := validateDefinitions(Definitions()); err != nil {
		return nil, err
	}
	catalogIDs, err := catalogIDsFromCommittedCatalog()
	if err != nil {
		return nil, err
	}
	bundle := i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("toml", toml.Unmarshal)
	if _, err := bundle.LoadMessageFileFS(localeFS, "locales/active.en.toml"); err != nil {
		return nil, fmt.Errorf("load English catalog: %w", err)
	}
	return &Renderer{
		localizer:       i18n.NewLocalizer(bundle, "en"),
		catalogMessages: catalogIDs,
	}, nil
}

func (r *Renderer) hasCatalogID(id string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.catalogMessages[id]
	return ok
}

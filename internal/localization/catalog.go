package localization

import (
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

type catalogMessage struct {
	Description string `toml:"description"`
	Other       string `toml:"other"`
}

var (
	ErrCommittedCatalogDefaultMismatch  = errors.New("committed catalog default mismatch")
	ErrCommittedCatalogMissingMessage   = errors.New("committed catalog missing message")
	ErrMessageDefinitionIDRequired      = errors.New("message definition ID is required")
	ErrMessageDefinitionDefaultMissing  = errors.New("message definition default is required")
	ErrMessageDefinitionDefaultConflict = errors.New("message definition default conflict")
	ErrEnglishCatalogRead               = errors.New("read English catalog")
	ErrEnglishCatalogParse              = errors.New("parse English catalog")
	ErrEnglishCatalogLoad               = errors.New("load English catalog")
)

func ValidateCommittedCatalog() error {
	definitions := Definitions()
	if err := validateDefinitions(definitions); err != nil {
		return err
	}
	catalog, err := committedCatalog()
	if err != nil {
		return err
	}
	var missing []string
	for _, definition := range definitions {
		entry, ok := catalog[definition.ID]
		if !ok {
			missing = append(missing, definition.ID)
			continue
		}
		if entry.Other != definition.Default {
			return fmt.Errorf("catalog %s other = %q, want %q: %w", definition.ID, entry.Other, definition.Default, ErrCommittedCatalogDefaultMismatch)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("catalog missing message IDs: %s: %w", strings.Join(missing, ", "), ErrCommittedCatalogMissingMessage)
	}
	return nil
}

func validateDefinitions(definitions []Definition) error {
	seen := map[string]Definition{}
	for _, definition := range definitions {
		id := strings.TrimSpace(definition.ID)
		if id == "" {
			return fmt.Errorf("message definition has empty ID: %w", ErrMessageDefinitionIDRequired)
		}
		if strings.TrimSpace(definition.Default) == "" {
			return fmt.Errorf("message definition %s has empty default: %w", id, ErrMessageDefinitionDefaultMissing)
		}
		if previous, ok := seen[id]; ok {
			if previous.Default != definition.Default {
				return fmt.Errorf("message ID %s has conflicting defaults %q and %q: %w", id, previous.Default, definition.Default, ErrMessageDefinitionDefaultConflict)
			}
			continue
		}
		seen[id] = definition
	}
	return nil
}

func catalogIDsFromCommittedCatalog() (map[string]struct{}, error) {
	catalog, err := committedCatalog()
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(catalog))
	for id := range catalog {
		ids[id] = struct{}{}
	}
	return ids, nil
}

func committedCatalog() (map[string]catalogMessage, error) {
	data, err := fs.ReadFile(localeFS, "locales/active.en.toml")
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEnglishCatalogRead, err)
	}
	var catalog map[string]catalogMessage
	if err := toml.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEnglishCatalogParse, err)
	}
	return catalog, nil
}

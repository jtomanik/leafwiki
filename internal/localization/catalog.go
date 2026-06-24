package localization

import (
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

type catalogMessage struct {
	Description string `toml:"description"`
	Other       string `toml:"other"`
}

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
			return fmt.Errorf("catalog %s other = %q, want %q", definition.ID, entry.Other, definition.Default)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("catalog missing message IDs: %s", strings.Join(missing, ", "))
	}
	return nil
}

func validateDefinitions(definitions []Definition) error {
	seen := map[string]Definition{}
	for _, definition := range definitions {
		id := strings.TrimSpace(definition.ID)
		if id == "" {
			return fmt.Errorf("message definition has empty ID")
		}
		if strings.TrimSpace(definition.Default) == "" {
			return fmt.Errorf("message definition %s has empty default", id)
		}
		if previous, ok := seen[id]; ok {
			if previous.Default != definition.Default {
				return fmt.Errorf("message ID %s has conflicting defaults %q and %q", id, previous.Default, definition.Default)
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
	data, err := localeFS.ReadFile("locales/active.en.toml")
	if err != nil {
		return nil, fmt.Errorf("read English catalog: %w", err)
	}
	var catalog map[string]catalogMessage
	if err := toml.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("parse English catalog: %w", err)
	}
	return catalog, nil
}

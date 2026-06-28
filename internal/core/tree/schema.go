package tree

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
)

const CurrentSchemaVersion = 5

type SchemaInfo struct {
	Version int `json:"version"`
}

func loadSchema(storageDir string) (SchemaInfo, error) {
	path := filepath.Join(storageDir, "schema.json")

	data, err := treeOSReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// First run / legacy install
			log.Printf("Schema file not found, assuming version 0")
			return SchemaInfo{Version: 0}, nil
		}
		log.Printf("Error reading schema file: %v", err)
		return SchemaInfo{}, err
	}

	var s SchemaInfo
	if err := treeJSONUnmarshal(data, &s); err != nil {
		log.Printf("Error unmarshaling schema file: %v", err)
		return SchemaInfo{}, err
	}

	return s, nil
}

func saveSchema(storageDir string, version int) error {
	path := filepath.Join(storageDir, "schema.json")
	s := SchemaInfo{Version: version}

	data, _ := json.MarshalIndent(s, "", "  ")

	log.Printf("Saving schema version %d to %s", version, path)
	return treeOSWriteFile(path, data, 0o644)
}

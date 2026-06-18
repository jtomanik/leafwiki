package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/perber/wiki/internal/wikid"
)

type grantInput struct {
	Subject     string `json:"subject"`
	WorkspaceID string `json:"workspaceId"`
	Role        string `json:"role"`
}

func main() {
	dbPath := flag.String("db-path", "", "wikid SQLite database path")
	globalDataDir := flag.String("global-data-dir", "", "LeafWiki global data directory")
	flag.Parse()
	storePath := *dbPath
	if storePath == "" {
		if *globalDataDir == "" {
			fatalf("--db-path or --global-data-dir is required")
		}
		storePath = wikid.GlobalLayout(*globalDataDir).DBPath
	}
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fatalf("read grants input: %v", err)
	}
	var inputs []grantInput
	if err := json.Unmarshal(raw, &inputs); err != nil {
		fatalf("decode grants input: %v", err)
	}
	store := wikid.NewGrantStore(storePath)
	for _, input := range inputs {
		if err := store.Upsert(wikid.Grant{
			Subject:     input.Subject,
			WorkspaceID: input.WorkspaceID,
			Role:        wikid.GrantRole(input.Role),
		}); err != nil {
			fatalf("upsert grant %s %s: %v", input.Subject, input.WorkspaceID, err)
		}
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

type grantStore interface {
	Upsert(wikid.Grant) error
}

type grantInput struct {
	Subject     string                  `json:"subject"`
	WorkspaceID workspaceid.WorkspaceID `json:"workspaceId"`
	Role        string                  `json:"role"`
}

var (
	grantWorkspaceArgs             = func() []string { return os.Args[1:] }
	grantWorkspaceStdin  io.Reader = os.Stdin
	grantWorkspaceStderr io.Writer = os.Stderr
	grantWorkspaceExit             = os.Exit
	newGrantStore                  = func(path string) grantStore {
		return wikid.NewGrantStore(path)
	}
)

func main() {
	grantWorkspaceExit(runGrantWorkspaces(grantWorkspaceArgs(), grantWorkspaceStdin, grantWorkspaceStderr))
}

func runGrantWorkspaces(args []string, stdin io.Reader, stderr io.Writer) int {
	flags := flag.NewFlagSet("grant-workspaces", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dbPath := flags.String("db-path", "", "wikid SQLite database path")
	globalDataDir := flags.String("global-data-dir", "", "LeafWiki global data directory")
	if err := flags.Parse(args); err != nil {
		return fatalf(stderr, "parse flags: %v", err)
	}
	storePath := *dbPath
	if storePath == "" {
		if *globalDataDir == "" {
			return fatalf(stderr, "--db-path or --global-data-dir is required")
		}
		storePath = wikid.GlobalLayout(*globalDataDir).DBPath
	}
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return fatalf(stderr, "read grants input: %v", err)
	}
	var inputs []grantInput
	if err := json.Unmarshal(raw, &inputs); err != nil {
		return fatalf(stderr, "decode grants input: %v", err)
	}
	store := newGrantStore(storePath)
	for _, input := range inputs {
		role, err := wikid.ParseGrantRole(input.Role)
		if err != nil {
			return fatalf(stderr, "parse grant role %s %s: %v", input.Subject, input.WorkspaceID, err)
		}
		if err := store.Upsert(wikid.Grant{
			Subject:     input.Subject,
			WorkspaceID: input.WorkspaceID,
			Role:        role,
		}); err != nil {
			return fatalf(stderr, "upsert grant %s %s: %v", input.Subject, input.WorkspaceID, err)
		}
	}
	return 0
}

func fatalf(stderr io.Writer, format string, args ...any) int {
	_, _ = fmt.Fprintf(stderr, format+"\n", args...)
	return 1
}

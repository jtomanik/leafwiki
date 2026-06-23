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

type grantInput struct {
	Subject     string                  `json:"subject"`
	WorkspaceID workspaceid.WorkspaceID `json:"workspaceId"`
	Role        string                  `json:"role"`
}

type registerWorkspaceInput struct {
	DisplayName            string `json:"displayName"`
	DataDir                string `json:"dataDir"`
	RootDir                string `json:"rootDir"`
	MarkdownLinkRootPrefix string `json:"markdownLinkRootPrefix"`
}

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: wikid-store <read-registry|register-workspace|upsert-grants|replace-subject-grants> [flags]")
	}
	command := os.Args[1]
	flags := flag.NewFlagSet(command, flag.ExitOnError)
	globalDataDir := flags.String("global-data-dir", "", "LeafWiki global data directory")
	subject := flags.String("subject", "", "grant subject to replace")
	if err := flags.Parse(os.Args[2:]); err != nil {
		fatalf("parse flags: %v", err)
	}
	if *globalDataDir == "" {
		fatalf("--global-data-dir is required")
	}
	layout := wikid.GlobalLayout(*globalDataDir)

	switch command {
	case "read-registry":
		doc, err := wikid.NewRegistryStore(layout.DBPath).Load()
		if err != nil {
			fatalf("load registry: %v", err)
		}
		writeJSON(doc)
	case "register-workspace":
		var input registerWorkspaceInput
		readJSON(&input)
		workspace, err := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout).RegisterWorkspace(wikid.RegisterWorkspaceRequest{
			DisplayName:            input.DisplayName,
			DataDir:                input.DataDir,
			RootDir:                input.RootDir,
			MarkdownLinkRootPrefix: input.MarkdownLinkRootPrefix,
		})
		if err != nil {
			fatalf("register workspace: %v", err)
		}
		writeJSON(workspace)
	case "upsert-grants":
		for _, grant := range readGrantInputs() {
			if err := wikid.NewGrantStore(layout.DBPath).Upsert(grant); err != nil {
				fatalf("upsert grant %s %s: %v", grant.Subject, grant.WorkspaceID, err)
			}
		}
	case "replace-subject-grants":
		if *subject == "" {
			fatalf("--subject is required")
		}
		if err := wikid.NewGrantStore(layout.DBPath).ReplaceSubjectGrants(*subject, readGrantInputs()); err != nil {
			fatalf("replace grants for %s: %v", *subject, err)
		}
	default:
		fatalf("unknown command %q", command)
	}
}

func readGrantInputs() []wikid.Grant {
	var inputs []grantInput
	readJSON(&inputs)
	grants := make([]wikid.Grant, 0, len(inputs))
	for _, input := range inputs {
		grants = append(grants, wikid.Grant{
			Subject:     input.Subject,
			WorkspaceID: input.WorkspaceID,
			Role:        wikid.GrantRole(input.Role),
		})
	}
	return grants
}

func readJSON(target any) {
	raw, err := io.ReadAll(os.Stdin)
	if err != nil {
		fatalf("read stdin: %v", err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		fatalf("decode stdin JSON: %v", err)
	}
}

func writeJSON(value any) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fatalf("encode stdout JSON: %v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

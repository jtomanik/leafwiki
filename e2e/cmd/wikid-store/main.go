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

type registryStore interface {
	Load() (wikid.RegistryDocument, error)
}

type registryService interface {
	RegisterWorkspace(wikid.RegisterWorkspaceRequest) (wikid.WorkspaceRecord, error)
}

type grantStore interface {
	Upsert(wikid.Grant) error
	ReplaceSubjectGrants(string, []wikid.Grant) error
}

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

var (
	wikidStoreArgs             = func() []string { return os.Args[1:] }
	wikidStoreStdin  io.Reader = os.Stdin
	wikidStoreStdout io.Writer = os.Stdout
	wikidStoreStderr io.Writer = os.Stderr
	wikidStoreExit             = os.Exit
	newRegistryStore           = func(path string) registryStore {
		return wikid.NewRegistryStore(path)
	}
	newRegistryService = func(path string, layout wikid.Layout) registryService {
		return wikid.NewRegistryService(wikid.NewRegistryStore(path), layout)
	}
	newGrantStore = func(path string) grantStore {
		return wikid.NewGrantStore(path)
	}
)

func main() {
	wikidStoreExit(runWikidStore(wikidStoreArgs(), wikidStoreStdin, wikidStoreStdout, wikidStoreStderr))
}

func runWikidStore(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	if len(args) < 1 {
		return fatalf(stderr, "usage: wikid-store <read-registry|register-workspace|upsert-grants|replace-subject-grants> [flags]")
	}
	command := args[0]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	globalDataDir := flags.String("global-data-dir", "", "LeafWiki global data directory")
	subject := flags.String("subject", "", "grant subject to replace")
	if err := flags.Parse(args[1:]); err != nil {
		return fatalf(stderr, "parse flags: %v", err)
	}
	if *globalDataDir == "" {
		return fatalf(stderr, "--global-data-dir is required")
	}
	layout := wikid.GlobalLayout(*globalDataDir)

	switch command {
	case "read-registry":
		doc, err := newRegistryStore(layout.DBPath).Load()
		if err != nil {
			return fatalf(stderr, "load registry: %v", err)
		}
		if err := writeJSON(stdout, doc); err != nil {
			return fatalf(stderr, "encode stdout JSON: %v", err)
		}
	case "register-workspace":
		var input registerWorkspaceInput
		if err := readJSON(stdin, &input); err != nil {
			return fatalf(stderr, "%v", err)
		}
		workspace, err := newRegistryService(layout.DBPath, layout).RegisterWorkspace(wikid.RegisterWorkspaceRequest{
			DisplayName:            input.DisplayName,
			DataDir:                input.DataDir,
			RootDir:                input.RootDir,
			MarkdownLinkRootPrefix: input.MarkdownLinkRootPrefix,
		})
		if err != nil {
			return fatalf(stderr, "register workspace: %v", err)
		}
		if err := writeJSON(stdout, workspace); err != nil {
			return fatalf(stderr, "encode stdout JSON: %v", err)
		}
	case "upsert-grants":
		grants, err := readGrantInputs(stdin)
		if err != nil {
			return fatalf(stderr, "%v", err)
		}
		store := newGrantStore(layout.DBPath)
		for _, grant := range grants {
			if err := store.Upsert(grant); err != nil {
				return fatalf(stderr, "upsert grant %s %s: %v", grant.Subject, grant.WorkspaceID, err)
			}
		}
	case "replace-subject-grants":
		if *subject == "" {
			return fatalf(stderr, "--subject is required")
		}
		grants, err := readGrantInputs(stdin)
		if err != nil {
			return fatalf(stderr, "%v", err)
		}
		if err := newGrantStore(layout.DBPath).ReplaceSubjectGrants(*subject, grants); err != nil {
			return fatalf(stderr, "replace grants for %s: %v", *subject, err)
		}
	default:
		return fatalf(stderr, "unknown command %q", command)
	}
	return 0
}

func readGrantInputs(stdin io.Reader) ([]wikid.Grant, error) {
	var inputs []grantInput
	if err := readJSON(stdin, &inputs); err != nil {
		return nil, err
	}
	grants := make([]wikid.Grant, 0, len(inputs))
	for _, input := range inputs {
		role, err := wikid.ParseGrantRole(input.Role)
		if err != nil {
			return nil, err
		}
		grants = append(grants, wikid.Grant{
			Subject:     input.Subject,
			WorkspaceID: input.WorkspaceID,
			Role:        role,
		})
	}
	return grants, nil
}

func readJSON(stdin io.Reader, target any) error {
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("decode stdin JSON: %w", err)
	}
	return nil
}

func writeJSON(stdout io.Writer, value any) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return err
	}
	return nil
}

func fatalf(stderr io.Writer, format string, args ...any) int {
	_, _ = fmt.Fprintf(stderr, format+"\n", args...)
	return 1
}

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/wikid"
)

type seededUser struct {
	ID       string            `json:"id"`
	Username string            `json:"username"`
	Email    string            `json:"email"`
	Role     string            `json:"role"`
	APIKeyID coreauth.APIKeyID `json:"apiKeyId"`
	APIKey   string            `json:"apiKey"`
}

type seedOutput struct {
	Admin        seededUser `json:"admin"`
	Editor       seededUser `json:"editor"`
	SecondEditor seededUser `json:"secondEditor"`
	Viewer       seededUser `json:"viewer"`
	Revoked      seededUser `json:"revoked"`
	Deleted      seededUser `json:"deleted"`
}

type seedUserService interface {
	InitDefaultAdmin(password string) error
	GetUserByUsername(username string) (*coreauth.User, error)
	CreateUser(username string, email string, password string, role string) (*coreauth.User, error)
	DeleteUser(userID coreauth.UserID) error
}

type seedAPIKeyService interface {
	CreateAPIKey(userID coreauth.UserID, name string, createdByUserID coreauth.UserID) (*coreauth.APIKeyCreateResult, error)
	RevokeAPIKey(userID coreauth.UserID, keyID coreauth.APIKeyID) error
}

type seedServices struct {
	users   seedUserService
	apiKeys seedAPIKeyService
	close   func() error
}

var (
	seedArgs                    = func() []string { return os.Args[1:] }
	seedStderr        io.Writer = os.Stderr
	seedExit                    = os.Exit
	seedMarshalIndent           = json.MarshalIndent
	seedWriteFile               = os.WriteFile
	openSeedServices            = func(dataDir string) (seedServices, error) {
		stores, err := wikid.OpenAuthStores(dataDir)
		if err != nil {
			return seedServices{}, err
		}
		users := coreauth.NewUserService(stores.Users)
		return seedServices{
			users:   users,
			apiKeys: coreauth.NewAPIKeyService(stores.APIKeys, users),
			close:   stores.Close,
		}, nil
	}
)

func main() {
	seedExit(runSeedMCPAPIKeys(seedArgs(), seedStderr))
}

func runSeedMCPAPIKeys(args []string, stderr io.Writer) int {
	flags := flag.NewFlagSet("seed-mcp-api-keys", flag.ContinueOnError)
	flags.SetOutput(stderr)
	dataDir := flags.String("data-dir", "", "LeafWiki data directory")
	outputPath := flags.String("output", "", "JSON output path")
	if err := flags.Parse(args); err != nil {
		return fatalf(stderr, "parse flags: %v", err)
	}
	if *dataDir == "" || *outputPath == "" {
		return fatalf(stderr, "--data-dir and --output are required")
	}

	if err := writeSeedMCPAPIKeysOutput(*dataDir, *outputPath); err != nil {
		return fatalf(stderr, "%v", err)
	}
	return 0
}

func writeSeedMCPAPIKeysOutput(dataDir string, outputPath string) error {
	out, err := seedMCPAPIKeys(dataDir)
	if err != nil {
		return fmt.Errorf("seed MCP API keys: %w", err)
	}

	raw, err := seedMarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal output: %w", err)
	}
	if err := seedWriteFile(outputPath, raw, 0o600); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

func seedMCPAPIKeys(dataDir string) (seedOutput, error) {
	if err := rejectRemovedRuntimeStackEnv(); err != nil {
		return seedOutput{}, err
	}

	services, err := openSeedServices(dataDir)
	if err != nil {
		return seedOutput{}, err
	}
	defer services.close()
	users := services.users
	if err := users.InitDefaultAdmin("admin"); err != nil {
		return seedOutput{}, fmt.Errorf("init admin: %w", err)
	}

	apiKeys := services.apiKeys

	admin, err := users.GetUserByUsername("admin")
	if err != nil {
		return seedOutput{}, fmt.Errorf("load admin: %w", err)
	}
	editor, err := createUser(users, "stdio-editor", "stdio-editor@example.com", coreauth.RoleEditor)
	if err != nil {
		return seedOutput{}, err
	}
	secondEditor, err := createUser(users, "stdio-second-editor", "stdio-second-editor@example.com", coreauth.RoleEditor)
	if err != nil {
		return seedOutput{}, err
	}
	viewer, err := createUser(users, "stdio-viewer", "stdio-viewer@example.com", coreauth.RoleViewer)
	if err != nil {
		return seedOutput{}, err
	}
	revoked, err := createUser(users, "stdio-revoked", "stdio-revoked@example.com", coreauth.RoleEditor)
	if err != nil {
		return seedOutput{}, err
	}
	deleted, err := createUser(users, "stdio-deleted", "stdio-deleted@example.com", coreauth.RoleEditor)
	if err != nil {
		return seedOutput{}, err
	}

	out := seedOutput{}
	if out.Admin, err = createKey(apiKeys, admin, "E2E STDIO admin"); err != nil {
		return seedOutput{}, err
	}
	if out.Editor, err = createKey(apiKeys, editor, "E2E STDIO editor"); err != nil {
		return seedOutput{}, err
	}
	if out.SecondEditor, err = createKey(apiKeys, secondEditor, "E2E STDIO second editor"); err != nil {
		return seedOutput{}, err
	}
	if out.Viewer, err = createKey(apiKeys, viewer, "E2E STDIO viewer"); err != nil {
		return seedOutput{}, err
	}
	if out.Revoked, err = createKey(apiKeys, revoked, "E2E STDIO revoked"); err != nil {
		return seedOutput{}, err
	}
	if out.Deleted, err = createKey(apiKeys, deleted, "E2E STDIO deleted"); err != nil {
		return seedOutput{}, err
	}
	if err := apiKeys.RevokeAPIKey(coreauth.UserIDFromString(revoked.ID), out.Revoked.APIKeyID); err != nil {
		return seedOutput{}, fmt.Errorf("revoke seeded key: %w", err)
	}
	if err := users.DeleteUser(coreauth.UserIDFromString(deleted.ID)); err != nil {
		return seedOutput{}, fmt.Errorf("delete seeded user: %w", err)
	}
	return out, nil
}

func rejectRemovedRuntimeStackEnv() error {
	for _, name := range []string{
		"LEAFWIKI_RUNTIME_STACK",
		"LEAFWIKI_RUN_MCP_RUNTIME_STACK",
	} {
		if _, ok := os.LookupEnv(name); ok {
			return removedEnvironmentVariableError{Name: name}
		}
	}
	return nil
}

type removedEnvironmentVariableError struct {
	Name string
}

func (err removedEnvironmentVariableError) Error() string {
	return fmt.Sprintf("unknown environment variable: %s", err.Name)
}

func createUser(users seedUserService, username, email, role string) (*coreauth.User, error) {
	user, err := users.CreateUser(username, email, "password", role)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", username, err)
	}
	return user, nil
}

func createKey(apiKeys seedAPIKeyService, user *coreauth.User, name string) (seededUser, error) {
	userID := coreauth.UserIDFromString(user.ID)
	created, err := apiKeys.CreateAPIKey(userID, name, userID)
	if err != nil {
		return seededUser{}, fmt.Errorf("create key for %s: %w", user.Username, err)
	}
	return seededUser{
		ID:       user.ID,
		Username: user.Username,
		Email:    user.Email,
		Role:     user.Role,
		APIKeyID: created.Key.ID,
		APIKey:   created.Secret,
	}, nil
}

func fatalf(stderr io.Writer, format string, args ...any) int {
	fmt.Fprintf(stderr, format+"\n", args...)
	return 1
}

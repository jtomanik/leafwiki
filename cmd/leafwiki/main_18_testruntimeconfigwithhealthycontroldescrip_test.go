package main

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/localization"
	"github.com/perber/wiki/internal/locking"
	"github.com/perber/wiki/internal/projectdaemon"
	"github.com/perber/wiki/internal/runtimeconfig"
	"github.com/perber/wiki/internal/wikid"
	"github.com/perber/wiki/internal/workspaceid"
)

func testRuntimeConfigWithHealthyControlDescriptor(recordHandler http.HandlerFunc) (leafwikiRuntimeConfig, func()) {
	ginkgo.GinkgoHelper()

	baseDir := leafwikiTempDir()
	leafwikiSetenv("HOME", filepath.Join(baseDir, "home"))
	dataDir := filepath.Join(baseDir, "data")
	rootDir := filepath.Join(baseDir, "content")
	Expect(os.MkdirAll(dataDir, 0o755)).To(Succeed())
	Expect(os.MkdirAll(rootDir, 0o755)).To(Succeed())
	cfg := testRuntimeConfig(dataDir, rootDir, freeTCPPort(), mcpTransports{}, true)
	ownerCfg, err := daemonRequestConfigForRuntime(cfg)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("daemon request config: %v", err))

	configHash, err := projectdaemon.ConfigHash(ownerCfg)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("config hash: %v", err))

	dataLock, err := locking.AcquireDataDirLock(ownerCfg.DataDir)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("acquire data lock: %v", err))

	rootLock, err := locking.AcquireRootDirLock(ownerCfg.RootDir)
	if err != nil {
		_ = dataLock.Release()
	}
	Expect(err).NotTo(HaveOccurred())

	token := "control-token"
	pid := os.Getpid()
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get(projectdaemon.ControlTokenHeader) != token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if req.Method == http.MethodGet && req.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(projectdaemon.DaemonHealth{
				OK:            true,
				SchemaVersion: projectdaemon.DescriptorSchemaVersion,
				PID:           pid,
				DataDir:       ownerCfg.DataDir,
				RootDir:       ownerCfg.RootDir,
				ConfigHash:    configHash,
			})
			return
		}
		if req.Method == http.MethodPost && req.URL.Path == "/agent-presence/events" {
			recordHandler(w, req)
			return
		}
		http.NotFound(w, req)
	}))
	err = projectdaemon.WriteDescriptorAtomic(projectdaemon.DescriptorPath(ownerCfg.DataDir), &projectdaemon.Descriptor{
		SchemaVersion:    projectdaemon.DescriptorSchemaVersion,
		PID:              pid,
		StartedAt:        time.Now().UTC(),
		DataDir:          ownerCfg.DataDir,
		RootDir:          ownerCfg.RootDir,
		PublicURL:        "http://127.0.0.1:" + ownerCfg.Port,
		PublicMCPEnabled: ownerCfg.PublicMCPEnabled,
		ControlURL:       control.URL,
		ConfigHash:       configHash,
		IdleTimeout:      ownerCfg.DaemonIdleTimeout,
		ControlToken:     token,
		Config:           ownerCfg,
	})
	if err != nil {
		control.Close()
		_ = rootLock.Release()
		_ = dataLock.Release()
	}
	Expect(err).NotTo(HaveOccurred())

	cleanup := func() {
		control.Close()
		_ = rootLock.Release()
		_ = dataLock.Release()
	}
	return cfg, cleanup
}

func readJSONLogEntries(path string) []map[string]any {
	ginkgo.GinkgoHelper()

	return readJSONLogEntriesFromText(readFileString(path))
}

func readJSONLogEntriesFromText(text string) []map[string]any {
	ginkgo.GinkgoHelper()

	entries := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(strings.TrimSpace(line), "{") {
			continue
		}
		var entry map[string]any
		Expect(json.Unmarshal([]byte(line), &entry)).To(Succeed(), line)
		entries = append(entries, entry)
	}
	return entries
}

func haveJSONLogEntry(msg string, matchers ...types.GomegaMatcher) types.GomegaMatcher {
	entryMatchers := []types.GomegaMatcher{
		HaveKey("time"),
		HaveKey("level"),
		HaveKey("source"),
		HaveKeyWithValue("msg", msg),
	}
	entryMatchers = append(entryMatchers, matchers...)
	return SatisfyAll(entryMatchers...)
}

func initWikidAdminUser(dataDir string) {
	ginkgo.GinkgoHelper()

	paths := wikid.AuthStoragePaths(dataDir)
	Expect(os.MkdirAll(paths.AuthDir, 0o755)).To(Succeed())
	store, err := coreauth.NewUserStore(paths.AuthDir)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("create wikid user store: %v", err))

	defer func() {
		Expect(store.Close()).To(Succeed(), fmt.Sprintf("close wikid user store: %v", err))
	}()
	service := coreauth.NewUserService(store)
	Expect(service.InitDefaultAdmin("old-password")).To(Succeed(), fmt.Sprintf("init wikid admin user: %v", err))
}

type testMCPAPIKey struct {
	Secret string
	UserID coreauth.UserID
}

func createWikidMCPAPIKey(dataDir string) string {
	ginkgo.GinkgoHelper()

	return createWikidMCPAPIKeyWithUser(dataDir).Secret
}

func createWikidMCPAPIKeyWithUser(dataDir string) testMCPAPIKey {
	ginkgo.GinkgoHelper()

	layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
	return createMCPAPIKeyInStorageDirWithUser(wikid.AuthStoragePaths(layout.HomeDir).AuthDir)
}

func createMCPAPIKeyInStorageDir(storageDir string) string {
	ginkgo.GinkgoHelper()

	return createMCPAPIKeyInStorageDirWithUser(storageDir).Secret
}

func createMCPAPIKeyInStorageDirWithUser(storageDir string) testMCPAPIKey {
	ginkgo.GinkgoHelper()

	Expect(os.MkdirAll(storageDir, 0o755)).To(Succeed())
	userStore, err := coreauth.NewUserStore(storageDir)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("create user store: %v", err))

	defer func() {
		Expect(userStore.Close()).To(Succeed(), fmt.Sprintf("close user store: %v", err))
	}()
	userService := coreauth.NewUserService(userStore)
	user, err := userService.CreateUser("editor", "editor@example.com", "password", coreauth.RoleEditor)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("create API-key user: %v", err))

	apiKeyStore, err := coreauth.NewAPIKeyStore(storageDir)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("create api key store: %v", err))

	apiKeyService := coreauth.NewAPIKeyService(apiKeyStore, userService)
	defer func() {
		Expect(apiKeyService.Close()).To(Succeed(), fmt.Sprintf("close api key service: %v", err))
	}()
	userID := coreauth.UserIDFromString(user.ID)
	created, err := apiKeyService.CreateAPIKey(userID, "Main process STDIO", userID)
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("create API key: %v", err))

	return testMCPAPIKey{Secret: created.Secret, UserID: coreauth.UserIDFromString(user.ID)}
}

func grantWikidWorkspaceAccessForDirs(dataDir string, rootDir string, userID coreauth.UserID, role wikid.GrantRole) {
	ginkgo.GinkgoHelper()

	layout := leafwikiHelperGlobalLayoutForDataDir(dataDir)
	registry := wikid.NewRegistryService(wikid.NewRegistryStore(layout.DBPath), layout)
	requestCfg := projectdaemon.Config{DataDir: dataDir, RootDir: rootDir}
	workspace, err := registry.RegisterWorkspace(wikid.RegisterWorkspaceRequest{
		DisplayName: federatedWorkspaceDisplayName(requestCfg),
		DataDir:     dataDir,
		RootDir:     rootDir,
	})
	Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("register workspace for grant: %v", err))

	grants := wikid.NewGrantStore(layout.DBPath)
	Expect(grants.Upsert(wikid.Grant{Subject: "user:" + userID.String(), WorkspaceID: workspace.ID, Role: role})).To(Succeed(), fmt.Sprintf("grant workspace access: %v", err))
}

func MatchCLIRenderedMessageError(messageID cliMessageID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(err error) (cliRenderedMessageError, error) {
		var cliErr cliRenderedMessageError
		if !errors.As(err, &cliErr) {
			return cliRenderedMessageError{}, fmt.Errorf("expected CLI rendered message error, got %T", err)
		}
		return cliErr, nil
	}, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"MessageID": Equal(messageID),
		"Message":   Equal(localization.English.Render(string(messageID), "").Message),
	}))
}

func MatchProjectDaemonWorkspaceIDMismatch(want, got workspaceid.WorkspaceID) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(err error) (*projectdaemon.ConfigMismatchError, error) {
		var mismatchErr *projectdaemon.ConfigMismatchError
		if !errors.As(err, &mismatchErr) {
			return nil, fmt.Errorf("expected project daemon config mismatch, got %T", err)
		}
		return mismatchErr, nil
	}, HaveField("Mismatches", ContainElement(Satisfy(func(mismatch projectdaemon.Mismatch) bool {
		wantID, wantErr := workspaceid.ParseWorkspaceID(mismatch.Want)
		gotID, gotErr := workspaceid.ParseWorkspaceID(mismatch.Got)
		return mismatch.Field == "workspace-id" &&
			wantErr == nil &&
			gotErr == nil &&
			wantID == want &&
			gotID == got
	}))))
}

func expectRuntimeConfigUsageReason(err error, reason runtimeconfig.ConfigUsageReason) {
	ginkgo.GinkgoHelper()

	Expect(err).To(MatchRuntimeConfigUsageReason(reason))
}

func expectRuntimeConfigFileReason(err error, reason runtimeconfig.ConfigFileErrorReason) {
	ginkgo.GinkgoHelper()

	Expect(err).To(MatchRuntimeConfigFileError(reason, ""))
}

func MatchRuntimeConfigUsageReason(reason runtimeconfig.ConfigUsageReason) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(err error) (runtimeconfig.ConfigUsageError, error) {
		var usage runtimeconfig.ConfigUsageError
		if !errors.As(err, &usage) {
			return runtimeconfig.ConfigUsageError{}, fmt.Errorf("expected runtime config usage error, got %T", err)
		}
		return usage, nil
	}, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Reason": Equal(reason),
	}))
}

func MatchRuntimeConfigFileError(reason runtimeconfig.ConfigFileErrorReason, key string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	fields := gstruct.Fields{
		"Reason": Equal(reason),
	}
	if key != "" {
		fields["Key"] = Equal(key)
	}
	return WithTransform(func(err error) (runtimeconfig.ConfigFileError, error) {
		var configErr runtimeconfig.ConfigFileError
		if !errors.As(err, &configErr) {
			return runtimeconfig.ConfigFileError{}, fmt.Errorf("expected runtime config file error, got %T", err)
		}
		return configErr, nil
	}, gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

func MatchMCPTransportError(reason runtimeconfig.MCPTransportErrorReason) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(func(err error) (runtimeconfig.MCPTransportError, error) {
		var transportErr runtimeconfig.MCPTransportError
		if !errors.As(err, &transportErr) {
			return runtimeconfig.MCPTransportError{}, fmt.Errorf("expected MCP transport error, got %T", err)
		}
		return transportErr, nil
	}, gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Reason": Equal(reason),
	}))
}

func removedStartupFlagName(arg string) string {
	name := strings.TrimLeft(arg, "-")
	name, _, _ = strings.Cut(name, "=")
	return name
}

func expectAgentHookConfigRejection(args []string) {
	ginkgo.GinkgoHelper()

	normalizedArgs := normalizeAgentHookRawArgs(args)
	rawUsageErr := runtimeconfig.ValidateRawConfigFlagUsage(normalizedArgs)
	if rawUsageErr != nil {
		expectRuntimeConfigUsageReason(rawUsageErr, runtimeconfig.ConfigUsageReasonConfigPathRequired)
		return
	}

	_, _, _, err := parseConfigFlagsForArgsAllowError(normalizedArgs)
	expectRuntimeConfigFileReason(err, runtimeconfig.ConfigFileErrorReasonRead)
}

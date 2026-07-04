package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/wikid"
)

var _ = ginkgo.Describe("seed MCP API keys", func() {
	ginkgo.It("default seams read process args and report auth store open errors", ginkgo.Label("integration"), func() {
		Expect(seedArgs()).NotTo(BeNil())
		blockedPath := filepath.Join(seedTempDir(), "not-a-dir")
		Expect(os.WriteFile(blockedPath, []byte("blocked"), 0o600)).To(Succeed())

		services, err := openSeedServices(blockedPath)

		Expect(err).To(MatchError(syscall.ENOTDIR))
		Expect(services.users).To(BeNil())
	})

	ginkgo.It("main delegates to the runner and exits with its status", ginkgo.Label("unit"), func() {
		unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
		unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
		restore := restoreSeedSeams()
		ginkgo.DeferCleanup(restore)
		var stderr bytes.Buffer
		var exitCode int
		users, apiKeys, services := newFakeSeedServices()
		seedArgs = func() []string {
			return []string{"--data-dir", "/tmp/data", "--output", "/tmp/seeds.json"}
		}
		seedStderr = &stderr
		seedExit = func(code int) {
			exitCode = code
			panic("exit")
		}
		openSeedServices = func(string) (seedServices, error) {
			return services, nil
		}
		seedWriteFile = func(string, []byte, os.FileMode) error {
			return nil
		}

		Expect(func() { main() }).To(PanicWith("exit"))

		Expect(exitCode).To(BeZero(), stderr.String())
		Expect(users.deleted).To(Equal(coreauth.UserIDFromString("stdio-deleted-id")))
		Expect(apiKeys.revokedKeyID).To(Equal(apiKeys.created["E2E STDIO revoked"].Key.ID))
	})

	ginkgo.DescribeTable("seed command failures", ginkgo.Label("unit"),
		func(tc runSeedErrorCase) {
			unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
			unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
			restore := restoreSeedSeams()
			ginkgo.DeferCleanup(restore)
			if tc.configure != nil {
				tc.configure()
			}
			var stderr bytes.Buffer

			code := runSeedMCPAPIKeys(tc.args, &stderr)

			Expect(code).To(Equal(1))
			Expect(seedCommandFailureReportFrom(stderr.String())).To(Equal(tc.wantFailure))
		},
		ginkgo.Entry("rejects unknown command flags", runSeedErrorCase{
			args:        []string{"--unknown"},
			wantFailure: seedCommandFailureReport{Kind: seedCommandFailureFlagParsing},
		}),
		ginkgo.Entry("requires data directory and output path arguments", runSeedErrorCase{
			wantFailure: seedCommandFailureReport{Kind: seedCommandFailureMissingPaths},
		}),
		ginkgo.Entry("reports output file write failures", runSeedErrorCase{
			args: []string{"--data-dir", "/tmp/data", "--output", "/tmp/seeds.json"},
			configure: func() {
				_, _, services := newFakeSeedServices()
				openSeedServices = func(string) (seedServices, error) {
					return services, nil
				}
				seedWriteFile = func(string, []byte, os.FileMode) error {
					return errors.New("write failed")
				}
			},
			wantFailure: seedCommandFailureReport{Kind: seedCommandFailureOutputWrite},
		}),
	)

	ginkgo.It("writes Wikid auth stores when legacy runtime stack variables are absent", ginkgo.Label("integration"), func() {
		unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
		unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
		dataDir := seedTempDir()

		seeds, err := seedMCPAPIKeys(dataDir)
		Expect(err).To(Succeed())

		Expect(seeds).To(HaveWikidSeededAPIKey(dataDir))
	})

	ginkgo.DescribeTable("removed runtime stack environment variables", ginkgo.Label("unit"),
		func(name string) {
			unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
			unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
			setEnvForTest(name, "legacy")

			_, err := seedMCPAPIKeys(seedTempDir())
			Expect(err).To(MatchError(removedEnvironmentVariableError{Name: name}))
		},
		ginkgo.Entry("rejects the legacy runtime stack variable", "LEAFWIKI_RUNTIME_STACK"),
		ginkgo.Entry("rejects the legacy MCP runtime stack variable", "LEAFWIKI_RUN_MCP_RUNTIME_STACK"),
	)

	ginkgo.It("writes Wikid auth stores for seeded API key principals", ginkgo.Label("integration"), func() {
		dataDir := seedTempDir()

		seeds, err := seedMCPAPIKeys(dataDir)
		Expect(err).To(Succeed())

		Expect(seeds).To(HaveWikidSeededAPIKey(dataDir))
	})

	ginkgo.It("accepts the environment when removed runtime stack variables are absent", ginkgo.Label("unit"), func() {
		unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
		unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")

		Expect(rejectRemovedRuntimeStackEnv()).To(Succeed())
	})

	ginkgo.It("seed output populates every principal and invalidates revoked and deleted keys", ginkgo.Label("integration"), func() {
		unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
		unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
		dataDir := seedTempDir()

		seeds, err := seedMCPAPIKeys(dataDir)
		Expect(err).To(Succeed())

		stores, err := wikid.OpenAuthStores(dataDir)
		Expect(err).To(Succeed())
		ginkgo.DeferCleanup(func() {
			Expect(stores.Close()).To(Succeed())
		})
		users := coreauth.NewUserService(stores.Users)
		apiKeys := coreauth.NewAPIKeyService(stores.APIKeys, users)

		ExpectSeededUser(seeds.Admin, "admin", coreauth.RoleAdmin)
		ExpectSeededUser(seeds.Editor, "stdio-editor", coreauth.RoleEditor)
		ExpectSeededUser(seeds.SecondEditor, "stdio-second-editor", coreauth.RoleEditor)
		ExpectSeededUser(seeds.Viewer, "stdio-viewer", coreauth.RoleViewer)
		ExpectSeededUser(seeds.Revoked, "stdio-revoked", coreauth.RoleEditor)
		ExpectSeededUser(seeds.Deleted, "stdio-deleted", coreauth.RoleEditor)

		for _, seeded := range []seededUser{seeds.Admin, seeds.Editor, seeds.SecondEditor, seeds.Viewer, seeds.Revoked} {
			user, err := users.GetUserByUsername(seeded.Username)
			Expect(err).To(Succeed())
			Expect(user).To(gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"ID":   Equal(seeded.ID),
				"Role": Equal(seeded.Role),
			})))
		}
		_, err = users.GetUserByUsername(seeds.Deleted.Username)
		Expect(err).To(MatchError(coreauth.ErrUserNotFound))

		_, err = apiKeys.VerifyAPIKey(seeds.Revoked.APIKey)
		Expect(err).To(MatchError(coreauth.ErrInvalidToken))
		_, err = apiKeys.VerifyAPIKey(seeds.Deleted.APIKey)
		Expect(err).To(MatchError(coreauth.ErrInvalidToken))
	})

	ginkgo.It("writes JSON seed output to the requested file", ginkgo.Label("integration"), func() {
		unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
		unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
		dataDir := seedTempDir()
		outputPath := filepath.Join(seedTempDir(), "seeds.json")

		Expect(writeSeedMCPAPIKeysOutput(dataDir, outputPath)).To(Succeed())
		info, err := os.Stat(outputPath)
		Expect(err).To(Succeed())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))

		raw, err := os.ReadFile(outputPath)
		Expect(err).To(Succeed())
		var seeds seedOutput
		Expect(json.Unmarshal(raw, &seeds)).To(Succeed())
		ExpectSeededUser(seeds.Admin, "admin", coreauth.RoleAdmin)
		ExpectSeededUser(seeds.Editor, "stdio-editor", coreauth.RoleEditor)
		Expect(seeds.Admin.APIKey).NotTo(Equal(seeds.Editor.APIKey))
	})

	ginkgo.DescribeTable("seed output failures", ginkgo.Label("unit"),
		func(tc seedOutputFailureCase) {
			unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
			unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
			restore := restoreSeedSeams()
			ginkgo.DeferCleanup(restore)
			tc.configure(tc.cause)

			err := writeSeedMCPAPIKeysOutput("/tmp/data", "/tmp/out.json")

			Expect(err).To(MatchError(tc.cause))
		},
		ginkgo.Entry("reports service opening failures before writing seeds", seedOutputFailureCase{
			configure: func(cause error) {
				openSeedServices = func(string) (seedServices, error) {
					return seedServices{}, cause
				}
			},
			cause: errors.New("open failed"),
		}),
		ginkgo.Entry("reports JSON encoding failures before writing seeds", seedOutputFailureCase{
			configure: func(cause error) {
				_, _, services := newFakeSeedServices()
				openSeedServices = func(string) (seedServices, error) {
					return services, nil
				}
				seedMarshalIndent = func(any, string, string) ([]byte, error) {
					return nil, cause
				}
			},
			cause: errors.New("marshal failed"),
		}),
		ginkgo.Entry("reports seed file write failures", seedOutputFailureCase{
			configure: func(cause error) {
				_, _, services := newFakeSeedServices()
				openSeedServices = func(string) (seedServices, error) {
					return services, nil
				}
				seedWriteFile = func(string, []byte, os.FileMode) error {
					return cause
				}
			},
			cause: errors.New("write failed"),
		}),
	)

	ginkgo.DescribeTable("seeded user setup failures", ginkgo.Label("unit"),
		func(tc seedServiceFailureCase) {
			unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
			unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
			restore := restoreSeedSeams()
			ginkgo.DeferCleanup(restore)
			users, apiKeys, services := newFakeSeedServices()
			tc.configure(users, apiKeys, tc.cause)
			openSeedServices = func(string) (seedServices, error) {
				return services, nil
			}

			_, err := seedMCPAPIKeys("/tmp/data")

			Expect(err).To(MatchError(tc.cause))
		},
		ginkgo.Entry("reports admin initialization failures", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) { users.initErr = cause },
			cause:     errors.New("init failed"),
		}),
		ginkgo.Entry("reports admin lookup failures", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) { users.getErr = cause },
			cause:     errors.New("load failed"),
		}),
		ginkgo.Entry("reports editor creation failures", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) {
				users.createErrFor["stdio-editor"] = cause
			},
			cause: errors.New("create failed"),
		}),
		ginkgo.Entry("reports second editor creation failures", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) {
				users.createErrFor["stdio-second-editor"] = cause
			},
			cause: errors.New("create failed"),
		}),
		ginkgo.Entry("reports viewer creation failures", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) {
				users.createErrFor["stdio-viewer"] = cause
			},
			cause: errors.New("create failed"),
		}),
		ginkgo.Entry("reports revoked-user creation failures", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) {
				users.createErrFor["stdio-revoked"] = cause
			},
			cause: errors.New("create failed"),
		}),
		ginkgo.Entry("reports deleted-user creation failures", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) {
				users.createErrFor["stdio-deleted"] = cause
			},
			cause: errors.New("create failed"),
		}),
		ginkgo.Entry("reports revoked-key revocation failures", seedServiceFailureCase{
			configure: func(_ *fakeSeedUsers, apiKeys *fakeSeedAPIKeys, cause error) { apiKeys.revokeErr = cause },
			cause:     errors.New("revoke failed"),
		}),
		ginkgo.Entry("reports deleted-user removal failures", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) { users.deleteErr = cause },
			cause:     errors.New("delete failed"),
		}),
	)

	ginkgo.DescribeTable("seeded API key creation failures", ginkgo.Label("unit"),
		func(tc seedAPIKeyCreationFailureCase) {
			unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
			unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
			restore := restoreSeedSeams()
			ginkgo.DeferCleanup(restore)
			_, apiKeys, services := newFakeSeedServices()
			keyFailedErr := errors.New("key failed")
			apiKeys.createErrFor[tc.keyName] = keyFailedErr
			openSeedServices = func(string) (seedServices, error) {
				return services, nil
			}

			_, err := seedMCPAPIKeys("/tmp/data")

			Expect(err).To(MatchError(keyFailedErr))
		},
		ginkgo.Entry("reports admin API key creation failures", seedAPIKeyCreationFailureCase{keyName: "E2E STDIO admin", user: "admin"}),
		ginkgo.Entry("reports editor API key creation failures", seedAPIKeyCreationFailureCase{keyName: "E2E STDIO editor", user: "stdio-editor"}),
		ginkgo.Entry("reports second editor API key creation failures", seedAPIKeyCreationFailureCase{keyName: "E2E STDIO second editor", user: "stdio-second-editor"}),
		ginkgo.Entry("reports viewer API key creation failures", seedAPIKeyCreationFailureCase{keyName: "E2E STDIO viewer", user: "stdio-viewer"}),
		ginkgo.Entry("reports revoked-user API key creation failures", seedAPIKeyCreationFailureCase{keyName: "E2E STDIO revoked", user: "stdio-revoked"}),
		ginkgo.Entry("reports deleted-user API key creation failures", seedAPIKeyCreationFailureCase{keyName: "E2E STDIO deleted", user: "stdio-deleted"}),
	)
})

type runSeedErrorCase struct {
	args        []string
	configure   func()
	wantFailure seedCommandFailureReport
}

type seedCommandFailureKind string

const (
	seedCommandFailureUnknown      seedCommandFailureKind = "unknown seed command failure"
	seedCommandFailureFlagParsing  seedCommandFailureKind = "seed command flag parsing failure"
	seedCommandFailureMissingPaths seedCommandFailureKind = "seed command missing required paths failure"
	seedCommandFailureOutputWrite  seedCommandFailureKind = "seed command output write failure"
)

type seedCommandFailureReport struct {
	Kind seedCommandFailureKind
}

func seedCommandFailureReportFrom(stderr string) seedCommandFailureReport {
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		line = strings.TrimSpace(line)
		if _, ok := strings.CutPrefix(line, "parse flags:"); ok {
			return seedCommandFailureReport{Kind: seedCommandFailureFlagParsing}
		}
		if line == "--data-dir and --output are required" {
			return seedCommandFailureReport{Kind: seedCommandFailureMissingPaths}
		}
		if _, ok := strings.CutPrefix(line, "write output:"); ok {
			return seedCommandFailureReport{Kind: seedCommandFailureOutputWrite}
		}
	}
	return seedCommandFailureReport{Kind: seedCommandFailureUnknown}
}

type seedOutputFailureCase struct {
	configure func(error)
	cause     error
}

type seedServiceFailureCase struct {
	configure func(*fakeSeedUsers, *fakeSeedAPIKeys, error)
	cause     error
}

type seedAPIKeyCreationFailureCase struct {
	keyName string
	user    string
}

func HaveWikidSeededAPIKey(dataDir string) types.GomegaMatcher {
	ginkgo.GinkgoHelper()

	return WithTransform(
		func(seeds seedOutput) wikidSeededAPIKeyObservation {
			return observeWikidSeededAPIKey(dataDir, seeds)
		},
		Equal(wikidSeededAPIKeyObservation{
			AuthStores:   wikidAuthStoresUseCurrentLayout,
			EditorAPIKey: seededEditorAPIKeyVerifies,
		}),
	)
}

type wikidAuthStoreLayout string

const (
	wikidAuthStoresUseCurrentLayout wikidAuthStoreLayout = "Wikid auth stores use current layout"
	wikidAuthStoresExposeLegacyDBs  wikidAuthStoreLayout = "legacy auth database files are present"
	wikidAuthStoreLayoutStatFailed  wikidAuthStoreLayout = "auth store layout inspection failed"
	wikidAuthStoreOpenFailed        wikidAuthStoreLayout = "Wikid auth stores could not be opened"
)

type seededAPIKeyVerification string

const (
	seededEditorAPIKeyUnchecked           seededAPIKeyVerification = "seeded editor API key was not checked"
	seededEditorAPIKeyVerifies            seededAPIKeyVerification = "seeded editor API key verifies"
	seededEditorAPIKeyRejected            seededAPIKeyVerification = "seeded editor API key is rejected"
	seededEditorAPIKeyUnexpectedPrincipal seededAPIKeyVerification = "seeded editor API key resolves to another principal"
)

type wikidSeededAPIKeyObservation struct {
	AuthStores   wikidAuthStoreLayout
	EditorAPIKey seededAPIKeyVerification
}

func observeWikidSeededAPIKey(dataDir string, seeds seedOutput) wikidSeededAPIKeyObservation {
	observed := wikidSeededAPIKeyObservation{EditorAPIKey: seededEditorAPIKeyUnchecked}
	for _, name := range []string{"users.db", "sessions.db", "api_keys.db"} {
		_, err := os.Stat(filepath.Join(dataDir, name))
		switch {
		case err == nil:
			observed.AuthStores = wikidAuthStoresExposeLegacyDBs
			return observed
		case errors.Is(err, os.ErrNotExist):
		default:
			observed.AuthStores = wikidAuthStoreLayoutStatFailed
			return observed
		}
	}

	stores, err := wikid.OpenAuthStores(dataDir)
	if err != nil {
		observed.AuthStores = wikidAuthStoreOpenFailed
		return observed
	}
	defer stores.Close()
	observed.AuthStores = wikidAuthStoresUseCurrentLayout
	users := coreauth.NewUserService(stores.Users)
	apiKeys := coreauth.NewAPIKeyService(stores.APIKeys, users)

	verified, err := apiKeys.VerifyAPIKey(seeds.Editor.APIKey)
	if err != nil {
		observed.EditorAPIKey = seededEditorAPIKeyRejected
		return observed
	}
	if verified.User.Username != seeds.Editor.Username {
		observed.EditorAPIKey = seededEditorAPIKeyUnexpectedPrincipal
		return observed
	}
	observed.EditorAPIKey = seededEditorAPIKeyVerifies
	return observed
}

func unsetEnvForTest(name string) {
	ginkgo.GinkgoHelper()
	value, ok := os.LookupEnv(name)
	Expect(os.Unsetenv(name)).To(Succeed())
	ginkgo.DeferCleanup(func() {
		if ok {
			Expect(os.Setenv(name, value)).To(Succeed())
		} else {
			Expect(os.Unsetenv(name)).To(Succeed())
		}
	})
}

func setEnvForTest(name string, value string) {
	ginkgo.GinkgoHelper()
	previousValue, ok := os.LookupEnv(name)
	Expect(os.Setenv(name, value)).To(Succeed())
	ginkgo.DeferCleanup(func() {
		if ok {
			Expect(os.Setenv(name, previousValue)).To(Succeed())
		} else {
			Expect(os.Unsetenv(name)).To(Succeed())
		}
	})
}

func seedTempDir() string {
	ginkgo.GinkgoHelper()
	dir, err := os.MkdirTemp("", "leafwiki-seed-mcp-api-keys-*")
	Expect(err).To(Succeed())
	ginkgo.DeferCleanup(os.RemoveAll, dir)
	return dir
}

func ExpectSeededUser(user seededUser, username string, role string) {
	ginkgo.GinkgoHelper()
	expectedEmail := username + "@example.com"
	if username == "admin" {
		expectedEmail = "admin@localhost"
	}
	Expect(user).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"ID":       Not(BeEmpty()),
		"Username": Equal(username),
		"Email":    Equal(expectedEmail),
		"Role":     Equal(role),
		"APIKeyID": Not(BeEmpty()),
		"APIKey": SatisfyAll(
			HavePrefix(coreauth.APIKeyPrefix),
			WithTransform(strings.TrimSpace, Equal(user.APIKey)),
		),
	}))
}

type fakeSeedUsers struct {
	users        map[string]*coreauth.User
	initErr      error
	getErr       error
	createErrFor map[string]error
	deleteErr    error
	deleted      coreauth.UserID
}

func (s *fakeSeedUsers) InitDefaultAdmin(string) error {
	if s.initErr != nil {
		return s.initErr
	}
	s.users["admin"] = &coreauth.User{ID: "admin-id", Username: "admin", Email: "admin@localhost", Role: coreauth.RoleAdmin}
	return nil
}

func (s *fakeSeedUsers) GetUserByUsername(username string) (*coreauth.User, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	user, ok := s.users[username]
	if !ok {
		return nil, coreauth.ErrUserNotFound
	}
	return user, nil
}

func (s *fakeSeedUsers) CreateUser(username string, email string, _ string, role string) (*coreauth.User, error) {
	if err := s.createErrFor[username]; err != nil {
		return nil, err
	}
	user := &coreauth.User{ID: username + "-id", Username: username, Email: email, Role: role}
	s.users[username] = user
	return user, nil
}

func (s *fakeSeedUsers) DeleteUser(userID coreauth.UserID) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}
	s.deleted = userID
	return nil
}

type fakeSeedAPIKeys struct {
	created      map[string]*coreauth.APIKeyCreateResult
	createErrFor map[string]error
	revokeErr    error
	revokedKeyID coreauth.APIKeyID
}

func (s *fakeSeedAPIKeys) CreateAPIKey(userID coreauth.UserID, name string, _ coreauth.UserID) (*coreauth.APIKeyCreateResult, error) {
	if err := s.createErrFor[name]; err != nil {
		return nil, err
	}
	result := &coreauth.APIKeyCreateResult{
		Key:    &coreauth.APIKey{ID: coreauth.APIKeyIDFromString(strings.ToLower(strings.ReplaceAll(name, " ", "-"))), UserID: userID, Name: name},
		Secret: coreauth.APIKeyPrefix + strings.ReplaceAll(name, " ", "_"),
	}
	s.created[name] = result
	return result, nil
}

func (s *fakeSeedAPIKeys) RevokeAPIKey(_ coreauth.UserID, keyID coreauth.APIKeyID) error {
	if s.revokeErr != nil {
		return s.revokeErr
	}
	s.revokedKeyID = keyID
	return nil
}

func newFakeSeedServices() (*fakeSeedUsers, *fakeSeedAPIKeys, seedServices) {
	users := &fakeSeedUsers{
		users:        map[string]*coreauth.User{},
		createErrFor: map[string]error{},
	}
	apiKeys := &fakeSeedAPIKeys{
		created:      map[string]*coreauth.APIKeyCreateResult{},
		createErrFor: map[string]error{},
	}
	return users, apiKeys, seedServices{
		users:   users,
		apiKeys: apiKeys,
		close:   func() error { return nil },
	}
}

func restoreSeedSeams() func() {
	previousArgs := seedArgs
	previousStderr := seedStderr
	previousExit := seedExit
	previousMarshalIndent := seedMarshalIndent
	previousWriteFile := seedWriteFile
	previousOpenSeedServices := openSeedServices
	return func() {
		seedArgs = previousArgs
		seedStderr = previousStderr
		seedExit = previousExit
		seedMarshalIndent = previousMarshalIndent
		seedWriteFile = previousWriteFile
		openSeedServices = previousOpenSeedServices
	}
}

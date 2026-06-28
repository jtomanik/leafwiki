package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/wikid"
)

var _ = ginkgo.Describe("seed MCP API keys", func() {
	ginkgo.It("default seams read process args and report auth store open errors", func() {
		Expect(seedArgs()).NotTo(BeNil())
		blockedPath := filepath.Join(ginkgo.GinkgoT().TempDir(), "not-a-dir")
		Expect(os.WriteFile(blockedPath, []byte("blocked"), 0o600)).To(Succeed())

		services, err := openSeedServices(blockedPath)

		Expect(err).To(HaveOccurred())
		Expect(services.users).To(BeNil())
	})

	ginkgo.It("main delegates to the runner and exits with its status", func() {
		unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
		unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
		restore := restoreSeedSeams()
		defer restore()
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

		Expect(exitCode).To(Equal(0), stderr.String())
		Expect(users.deleted).To(Equal(coreauth.UserIDFromString("stdio-deleted-id")))
		Expect(apiKeys.revokedKeyID).To(Equal(apiKeys.created["E2E STDIO revoked"].Key.ID))
	})

	ginkgo.It("runSeedMCPAPIKeys reports command line and output errors", func() {
		cases := []struct {
			name      string
			args      []string
			configure func()
			want      string
		}{
			{name: "parse", args: []string{"--unknown"}, want: "parse flags:"},
			{name: "missing required", args: nil, want: "--data-dir and --output are required"},
			{
				name: "write output",
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
				want: "write output: write failed",
			},
		}

		for _, tc := range cases {
			tc := tc
			ginkgo.By(tc.name)
			unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
			unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
			restore := restoreSeedSeams()
			func() {
				defer restore()
				if tc.configure != nil {
					tc.configure()
				}
				var stderr bytes.Buffer

				code := runSeedMCPAPIKeys(tc.args, &stderr)

				Expect(code).To(Equal(1))
				Expect(stderr.String()).To(ContainSubstring(tc.want))
			}()
		}
	})

	ginkgo.It("TestSeedMCPAPIKeysWritesWikidAuthStoreByDefault", func() {
		unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
		unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
		dataDir := ginkgo.GinkgoT().TempDir()

		seeds, err := seedMCPAPIKeys(dataDir)
		Expect(err).NotTo(HaveOccurred())

		assertWikidSeededAPIKey(dataDir, seeds)
	})

	ginkgo.DescribeTable("TestSeedMCPAPIKeysRejectsRemovedRuntimeStackEnvironment",
		func(name string) {
			unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
			unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
			Expect(os.Setenv(name, "legacy")).To(Succeed())

			_, err := seedMCPAPIKeys(ginkgo.GinkgoT().TempDir())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("unknown environment variable: " + name))
		},
		ginkgo.Entry("LEAFWIKI_RUNTIME_STACK", "LEAFWIKI_RUNTIME_STACK"),
		ginkgo.Entry("LEAFWIKI_RUN_MCP_RUNTIME_STACK", "LEAFWIKI_RUN_MCP_RUNTIME_STACK"),
	)

	ginkgo.It("TestSeedMCPAPIKeysWritesWikidAuthStore", func() {
		dataDir := ginkgo.GinkgoT().TempDir()

		seeds, err := seedMCPAPIKeys(dataDir)
		Expect(err).NotTo(HaveOccurred())

		assertWikidSeededAPIKey(dataDir, seeds)
	})

	ginkgo.It("rejectRemovedRuntimeStackEnv succeeds when removed variables are unset", func() {
		unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
		unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")

		Expect(rejectRemovedRuntimeStackEnv()).To(Succeed())
	})

	ginkgo.It("seed output populates every principal and invalidates revoked and deleted keys", func() {
		unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
		unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
		dataDir := ginkgo.GinkgoT().TempDir()

		seeds, err := seedMCPAPIKeys(dataDir)
		Expect(err).NotTo(HaveOccurred())

		stores, err := wikid.OpenAuthStores(dataDir)
		Expect(err).NotTo(HaveOccurred())
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
			Expect(err).NotTo(HaveOccurred())
			Expect(user.ID).To(Equal(seeded.ID))
			Expect(user.Role).To(Equal(seeded.Role))
		}
		_, err = users.GetUserByUsername(seeds.Deleted.Username)
		Expect(err).To(MatchError(coreauth.ErrUserNotFound))

		_, err = apiKeys.VerifyAPIKey(seeds.Revoked.APIKey)
		Expect(err).To(MatchError(coreauth.ErrInvalidToken))
		_, err = apiKeys.VerifyAPIKey(seeds.Deleted.APIKey)
		Expect(err).To(MatchError(coreauth.ErrInvalidToken))
	})

	ginkgo.It("writes JSON seed output to the requested file", func() {
		unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
		unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
		dataDir := ginkgo.GinkgoT().TempDir()
		outputPath := filepath.Join(ginkgo.GinkgoT().TempDir(), "seeds.json")

		Expect(writeSeedMCPAPIKeysOutput(dataDir, outputPath)).To(Succeed())
		info, err := os.Stat(outputPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0o600)))

		raw, err := os.ReadFile(outputPath)
		Expect(err).NotTo(HaveOccurred())
		var seeds seedOutput
		Expect(json.Unmarshal(raw, &seeds)).To(Succeed())
		ExpectSeededUser(seeds.Admin, "admin", coreauth.RoleAdmin)
		ExpectSeededUser(seeds.Editor, "stdio-editor", coreauth.RoleEditor)
		Expect(seeds.Admin.APIKey).NotTo(Equal(seeds.Editor.APIKey))
	})

	ginkgo.It("writeSeedMCPAPIKeysOutput wraps seed, marshal, and write failures", func() {
		unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
		unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
		restore := restoreSeedSeams()
		openSeedServices = func(string) (seedServices, error) {
			return seedServices{}, errors.New("open failed")
		}
		err := writeSeedMCPAPIKeysOutput("/tmp/data", "/tmp/out.json")
		Expect(err).To(MatchError(ContainSubstring("seed MCP API keys: open failed")))
		restore()

		restore = restoreSeedSeams()
		_, _, services := newFakeSeedServices()
		openSeedServices = func(string) (seedServices, error) {
			return services, nil
		}
		seedMarshalIndent = func(any, string, string) ([]byte, error) {
			return nil, errors.New("marshal failed")
		}
		err = writeSeedMCPAPIKeysOutput("/tmp/data", "/tmp/out.json")
		Expect(err).To(MatchError(ContainSubstring("marshal output: marshal failed")))
		restore()

		restore = restoreSeedSeams()
		_, _, services = newFakeSeedServices()
		openSeedServices = func(string) (seedServices, error) {
			return services, nil
		}
		seedWriteFile = func(string, []byte, os.FileMode) error {
			return errors.New("write failed")
		}
		err = writeSeedMCPAPIKeysOutput("/tmp/data", "/tmp/out.json")
		Expect(err).To(MatchError(ContainSubstring("write output: write failed")))
		restore()
	})

	ginkgo.It("seedMCPAPIKeys reports service setup and user mutation failures", func() {
		cases := []struct {
			name      string
			configure func(*fakeSeedUsers, *fakeSeedAPIKeys)
			want      string
		}{
			{name: "init admin", configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys) { users.initErr = errors.New("init failed") }, want: "init admin: init failed"},
			{name: "load admin", configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys) { users.getErr = errors.New("load failed") }, want: "load admin: load failed"},
			{name: "create editor", configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys) {
				users.createErrFor["stdio-editor"] = errors.New("create failed")
			}, want: "create stdio-editor: create failed"},
			{name: "create second editor", configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys) {
				users.createErrFor["stdio-second-editor"] = errors.New("create failed")
			}, want: "create stdio-second-editor: create failed"},
			{name: "create viewer", configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys) {
				users.createErrFor["stdio-viewer"] = errors.New("create failed")
			}, want: "create stdio-viewer: create failed"},
			{name: "create revoked", configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys) {
				users.createErrFor["stdio-revoked"] = errors.New("create failed")
			}, want: "create stdio-revoked: create failed"},
			{name: "create deleted", configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys) {
				users.createErrFor["stdio-deleted"] = errors.New("create failed")
			}, want: "create stdio-deleted: create failed"},
			{name: "revoke key", configure: func(_ *fakeSeedUsers, apiKeys *fakeSeedAPIKeys) { apiKeys.revokeErr = errors.New("revoke failed") }, want: "revoke seeded key: revoke failed"},
			{name: "delete user", configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys) { users.deleteErr = errors.New("delete failed") }, want: "delete seeded user: delete failed"},
		}

		for _, tc := range cases {
			tc := tc
			ginkgo.By(tc.name)
			unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
			unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
			restore := restoreSeedSeams()
			func() {
				defer restore()
				users, apiKeys, services := newFakeSeedServices()
				tc.configure(users, apiKeys)
				openSeedServices = func(string) (seedServices, error) {
					return services, nil
				}

				_, err := seedMCPAPIKeys("/tmp/data")

				Expect(err).To(MatchError(ContainSubstring(tc.want)))
			}()
		}
	})

	ginkgo.It("seedMCPAPIKeys reports every seeded API key creation failure", func() {
		cases := []struct {
			keyName string
			user    string
		}{
			{keyName: "E2E STDIO admin", user: "admin"},
			{keyName: "E2E STDIO editor", user: "stdio-editor"},
			{keyName: "E2E STDIO second editor", user: "stdio-second-editor"},
			{keyName: "E2E STDIO viewer", user: "stdio-viewer"},
			{keyName: "E2E STDIO revoked", user: "stdio-revoked"},
			{keyName: "E2E STDIO deleted", user: "stdio-deleted"},
		}

		for _, tc := range cases {
			tc := tc
			ginkgo.By(tc.keyName)
			unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
			unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
			restore := restoreSeedSeams()
			func() {
				defer restore()
				_, apiKeys, services := newFakeSeedServices()
				apiKeys.createErrFor[tc.keyName] = errors.New("key failed")
				openSeedServices = func(string) (seedServices, error) {
					return services, nil
				}

				_, err := seedMCPAPIKeys("/tmp/data")

				Expect(err).To(MatchError(ContainSubstring("create key for " + tc.user + ": key failed")))
			}()
		}
	})
})

func assertWikidSeededAPIKey(dataDir string, seeds seedOutput) {
	ginkgo.GinkgoHelper()
	for _, name := range []string{"users.db", "sessions.db", "api_keys.db"} {
		_, err := os.Stat(filepath.Join(dataDir, name))
		Expect(errors.Is(err, os.ErrNotExist)).To(BeTrue(), "legacy auth DB %s stat err = %v, want not exist", name, err)
	}

	stores, err := wikid.OpenAuthStores(dataDir)
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(stores.Close()).To(Succeed())
	})
	users := coreauth.NewUserService(stores.Users)
	apiKeys := coreauth.NewAPIKeyService(stores.APIKeys, users)

	verified, err := apiKeys.VerifyAPIKey(seeds.Editor.APIKey)
	Expect(err).NotTo(HaveOccurred())
	Expect(verified.User.Username).To(Equal(seeds.Editor.Username))
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

func ExpectSeededUser(user seededUser, username string, role string) {
	ginkgo.GinkgoHelper()
	Expect(user.ID).NotTo(BeEmpty())
	Expect(user.Username).To(Equal(username))
	if username == "admin" {
		Expect(user.Email).To(Equal("admin@localhost"))
	} else {
		Expect(user.Email).To(Equal(username + "@example.com"))
	}
	Expect(user.Role).To(Equal(role))
	Expect(user.APIKeyID).NotTo(BeEmpty())
	Expect(user.APIKey).To(HavePrefix(coreauth.APIKeyPrefix))
	Expect(strings.TrimSpace(user.APIKey)).To(Equal(user.APIKey))
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

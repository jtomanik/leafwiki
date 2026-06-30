package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gcustom"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

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

	ginkgo.DescribeTable("runSeedMCPAPIKeys reports command line and output errors",
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
			Expect(stderr.String()).To(ContainSubstring(tc.want))
		},
		ginkgo.Entry("parse", runSeedErrorCase{args: []string{"--unknown"}, want: "parse flags:"}),
		ginkgo.Entry("missing required", runSeedErrorCase{want: "--data-dir and --output are required"}),
		ginkgo.Entry("write output", runSeedErrorCase{
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
		}),
	)

	ginkgo.It("TestSeedMCPAPIKeysWritesWikidAuthStoreByDefault", func() {
		unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
		unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
		dataDir := ginkgo.GinkgoT().TempDir()

		seeds, err := seedMCPAPIKeys(dataDir)
		Expect(err).NotTo(HaveOccurred())

		Expect(seeds).To(HaveWikidSeededAPIKey(dataDir))
	})

	ginkgo.DescribeTable("TestSeedMCPAPIKeysRejectsRemovedRuntimeStackEnvironment",
		func(name string) {
			unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
			unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
			ginkgo.GinkgoT().Setenv(name, "legacy")

			_, err := seedMCPAPIKeys(ginkgo.GinkgoT().TempDir())
			Expect(err).To(HaveOccurred())
			Expect(err).To(MatchError(removedEnvironmentVariableError{Name: name}))
		},
		ginkgo.Entry("LEAFWIKI_RUNTIME_STACK", "LEAFWIKI_RUNTIME_STACK"),
		ginkgo.Entry("LEAFWIKI_RUN_MCP_RUNTIME_STACK", "LEAFWIKI_RUN_MCP_RUNTIME_STACK"),
	)

	ginkgo.It("TestSeedMCPAPIKeysWritesWikidAuthStore", func() {
		dataDir := ginkgo.GinkgoT().TempDir()

		seeds, err := seedMCPAPIKeys(dataDir)
		Expect(err).NotTo(HaveOccurred())

		Expect(seeds).To(HaveWikidSeededAPIKey(dataDir))
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

	ginkgo.DescribeTable("writeSeedMCPAPIKeysOutput wraps failures",
		func(tc seedOutputFailureCase) {
			unsetEnvForTest("LEAFWIKI_RUNTIME_STACK")
			unsetEnvForTest("LEAFWIKI_RUN_MCP_RUNTIME_STACK")
			restore := restoreSeedSeams()
			ginkgo.DeferCleanup(restore)
			tc.configure(tc.cause)

			err := writeSeedMCPAPIKeysOutput("/tmp/data", "/tmp/out.json")

			Expect(err).To(MatchError(tc.cause))
		},
		ginkgo.Entry("seed", seedOutputFailureCase{
			configure: func(cause error) {
				openSeedServices = func(string) (seedServices, error) {
					return seedServices{}, cause
				}
			},
			cause: errors.New("open failed"),
		}),
		ginkgo.Entry("marshal", seedOutputFailureCase{
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
		ginkgo.Entry("write", seedOutputFailureCase{
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

	ginkgo.DescribeTable("seedMCPAPIKeys reports service setup and user mutation failures",
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
		ginkgo.Entry("init admin", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) { users.initErr = cause },
			cause:     errors.New("init failed"),
		}),
		ginkgo.Entry("load admin", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) { users.getErr = cause },
			cause:     errors.New("load failed"),
		}),
		ginkgo.Entry("create editor", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) {
				users.createErrFor["stdio-editor"] = cause
			},
			cause: errors.New("create failed"),
		}),
		ginkgo.Entry("create second editor", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) {
				users.createErrFor["stdio-second-editor"] = cause
			},
			cause: errors.New("create failed"),
		}),
		ginkgo.Entry("create viewer", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) {
				users.createErrFor["stdio-viewer"] = cause
			},
			cause: errors.New("create failed"),
		}),
		ginkgo.Entry("create revoked", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) {
				users.createErrFor["stdio-revoked"] = cause
			},
			cause: errors.New("create failed"),
		}),
		ginkgo.Entry("create deleted", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) {
				users.createErrFor["stdio-deleted"] = cause
			},
			cause: errors.New("create failed"),
		}),
		ginkgo.Entry("revoke key", seedServiceFailureCase{
			configure: func(_ *fakeSeedUsers, apiKeys *fakeSeedAPIKeys, cause error) { apiKeys.revokeErr = cause },
			cause:     errors.New("revoke failed"),
		}),
		ginkgo.Entry("delete user", seedServiceFailureCase{
			configure: func(users *fakeSeedUsers, _ *fakeSeedAPIKeys, cause error) { users.deleteErr = cause },
			cause:     errors.New("delete failed"),
		}),
	)

	ginkgo.DescribeTable("seedMCPAPIKeys reports every seeded API key creation failure",
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
		ginkgo.Entry("admin", seedAPIKeyCreationFailureCase{keyName: "E2E STDIO admin", user: "admin"}),
		ginkgo.Entry("editor", seedAPIKeyCreationFailureCase{keyName: "E2E STDIO editor", user: "stdio-editor"}),
		ginkgo.Entry("second editor", seedAPIKeyCreationFailureCase{keyName: "E2E STDIO second editor", user: "stdio-second-editor"}),
		ginkgo.Entry("viewer", seedAPIKeyCreationFailureCase{keyName: "E2E STDIO viewer", user: "stdio-viewer"}),
		ginkgo.Entry("revoked", seedAPIKeyCreationFailureCase{keyName: "E2E STDIO revoked", user: "stdio-revoked"}),
		ginkgo.Entry("deleted", seedAPIKeyCreationFailureCase{keyName: "E2E STDIO deleted", user: "stdio-deleted"}),
	)
})

type runSeedErrorCase struct {
	args      []string
	configure func()
	want      string
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

	return gcustom.MakeMatcher(func(seeds seedOutput) (bool, error) {
		for _, name := range []string{"users.db", "sessions.db", "api_keys.db"} {
			_, err := os.Stat(filepath.Join(dataDir, name))
			if !errors.Is(err, os.ErrNotExist) {
				return false, fmt.Errorf("legacy auth DB %s stat err = %w", name, err)
			}
		}

		stores, err := wikid.OpenAuthStores(dataDir)
		if err != nil {
			return false, err
		}
		defer stores.Close()
		users := coreauth.NewUserService(stores.Users)
		apiKeys := coreauth.NewAPIKeyService(stores.APIKeys, users)

		verified, err := apiKeys.VerifyAPIKey(seeds.Editor.APIKey)
		if err != nil {
			return false, err
		}
		return verified.User.Username == seeds.Editor.Username, nil
	}).WithMessage("verify Wikid seeded API key")
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

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	coreauth "github.com/perber/wiki/internal/core/auth"
	"github.com/perber/wiki/internal/wikid"
)

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
	defer func() {
		Expect(stores.Close()).To(Succeed())
	}()
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

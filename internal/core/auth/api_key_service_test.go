package auth

import (
	"database/sql"
	"errors"
	"fmt"
	ginkgo "github.com/onsi/ginkgo/v2"
	"path/filepath"
	"strings"
	"time"
)

func setupTestAPIKeyService(t authTestT) (string, *UserService, *APIKeyStore, *APIKeyService, *User) {
	t.Helper()

	storageDir := t.TempDir()
	userStore, err := NewUserStore(storageDir)
	if err != nil {
		t.Fatalf("NewUserStore failed: %v", err)
	}
	t.Cleanup(func() { closeWithErrorCheck(userStore.Close) })

	userService := NewUserService(userStore)
	apiKeyStore, err := NewAPIKeyStore(storageDir)
	if err != nil {
		t.Fatalf("NewAPIKeyStore failed: %v", err)
	}
	t.Cleanup(func() { closeWithErrorCheck(apiKeyStore.Close) })

	apiKeyService := NewAPIKeyService(apiKeyStore, userService)
	user, err := userService.CreateUser("editor", "editor@example.com", "password123", RoleEditor)
	if err != nil {
		t.Fatalf("CreateUser failed: %v", err)
	}
	return storageDir, userService, apiKeyStore, apiKeyService, user
}

type sqliteExclusiveBlocker struct {
	db *sql.DB
}

func beginExclusiveSQLiteTransaction(t authTestT, path string) sqliteExclusiveBlocker {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open blocking connection: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.Exec("ROLLBACK")
		_ = db.Close()
	})
	if _, err := db.Exec("BEGIN EXCLUSIVE"); err != nil {
		t.Fatalf("begin blocking transaction: %v", err)
	}
	return sqliteExclusiveBlocker{db: db}
}

func (b sqliteExclusiveBlocker) rollback(t authTestT) {
	t.Helper()
	if _, err := b.db.Exec("ROLLBACK"); err != nil {
		t.Fatalf("rollback blocking transaction: %v", err)
	}
}

var _ = ginkgo.Describe("api key service", func() {
	ginkgo.It("TestAPIKeyServiceCreateStoresOnlyHashAndListsMetadata", func() {
		t := ginkgo.GinkgoT()
		_, _, store, service, user := setupTestAPIKeyService(t)
		userID := newFixtureUserID(user.ID)

		created, err := service.CreateAPIKey(userID, "  Local Codex  ", userID)
		if err != nil {
			t.Fatalf("CreateAPIKey failed: %v", err)
		}

		if created.Secret == "" || !strings.HasPrefix(created.Secret, "lwk_"+created.Key.ID.String()+"_") {
			t.Fatalf("secret = %q, want lwk_<id>_<secret>", created.Secret)
		}
		var _ APIKeyID = created.Key.ID
		if created.Key.Name != "Local Codex" {
			t.Fatalf("key name = %q, want trimmed name", created.Key.Name)
		}
		if got, want := created.Key.UserID, userID; got != want {
			t.Fatalf("key user id = %q, want %q", got, want)
		}
		if got, want := created.Key.CreatedByUserID, userID; got != want {
			t.Fatalf("createdBy = %q, want %q", got, want)
		}
		if got, want := created.Key.Scopes, []string{MCPAPIKeyScope}; len(got) != len(want) || got[0] != want[0] {
			t.Fatalf("scopes = %#v, want %#v", got, want)
		}
		if created.Key.Prefix == "" || created.Key.Last4 == "" {
			t.Fatalf("prefix/last4 must be set: %#v", created.Key)
		}
		if created.Key.CreatedAt.IsZero() {
			t.Fatalf("createdAt must be set")
		}
		if created.Key.LastUsedAt != nil || created.Key.RevokedAt != nil {
			t.Fatalf("new key lastUsedAt/revokedAt = %#v/%#v, want nil", created.Key.LastUsedAt, created.Key.RevokedAt)
		}

		var rawSecretCount int
		if err := store.db.QueryRow(`SELECT COUNT(*) FROM api_keys WHERE secret_hash LIKE '%' || ? || '%'`, created.Secret).Scan(&rawSecretCount); err != nil {
			t.Fatalf("query raw secret count: %v", err)
		}
		if rawSecretCount != 0 {
			t.Fatalf("database contains raw secret")
		}

		listed, err := service.ListAPIKeys(userID)
		if err != nil {
			t.Fatalf("ListAPIKeys failed: %v", err)
		}
		if len(listed) != 1 {
			t.Fatalf("listed keys = %d, want 1", len(listed))
		}
		if listed[0].ID != created.Key.ID || listed[0].Name != "Local Codex" {
			t.Fatalf("listed key = %#v, want created metadata", listed[0])
		}
	})

	ginkgo.It("TestAPIKeyServiceVerifyRejectsMalformedWrongSecretRevokedAndDeletedUser", func() {
		t := ginkgo.GinkgoT()
		_, userService, _, service, user := setupTestAPIKeyService(t)
		userID := newFixtureUserID(user.ID)

		created, err := service.CreateAPIKey(userID, "MCP client", userID)
		if err != nil {
			t.Fatalf("CreateAPIKey failed: %v", err)
		}

		verified, err := service.VerifyAPIKey(created.Secret)
		if err != nil {
			t.Fatalf("VerifyAPIKey failed: %v", err)
		}
		if verified.User.ID != user.ID || verified.User.Role != RoleEditor {
			t.Fatalf("verified user = %#v, want current editor", verified.User)
		}
		if verified.Key.LastUsedAt == nil {
			t.Fatalf("VerifyAPIKey should update lastUsedAt")
		}

		badInputs := []string{
			"",
			"not-an-api-key",
			"lwk_missing_parts",
			fmt.Sprintf("lwk_%s_wrongsecret", created.Key.ID),
		}
		for _, input := range badInputs {
			if _, err := service.VerifyAPIKey(input); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("VerifyAPIKey(%q) err = %v, want ErrInvalidToken", input, err)
			}
		}

		if err := service.RevokeAPIKey(userID, created.Key.ID); err != nil {
			t.Fatalf("RevokeAPIKey failed: %v", err)
		}
		if _, err := service.VerifyAPIKey(created.Secret); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("revoked VerifyAPIKey err = %v, want ErrInvalidToken", err)
		}
		listed, err := service.ListAPIKeys(userID)
		if err != nil {
			t.Fatalf("ListAPIKeys failed: %v", err)
		}
		if len(listed) != 0 {
			t.Fatalf("revoked key listed as active: %#v", listed)
		}

		second, err := service.CreateAPIKey(userID, "After revoke", userID)
		if err != nil {
			t.Fatalf("CreateAPIKey second failed: %v", err)
		}
		if _, err := userService.UpdateUser(userID, user.Username, user.Email, "", RoleViewer); err != nil {
			t.Fatalf("UpdateUser role failed: %v", err)
		}
		verified, err = service.VerifyAPIKey(second.Secret)
		if err != nil {
			t.Fatalf("VerifyAPIKey after role update failed: %v", err)
		}
		if verified.User.Role != RoleViewer {
			t.Fatalf("verified role = %q, want current viewer role", verified.User.Role)
		}
		if err := userService.DeleteUser(userID); err != nil {
			t.Fatalf("DeleteUser failed: %v", err)
		}
		if _, err := service.VerifyAPIKey(second.Secret); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("deleted-user VerifyAPIKey err = %v, want ErrInvalidToken", err)
		}
	})

	ginkgo.It("TestAPIKeyServiceVerifyRetriesTransientLastUsedLock", func() {
		t := ginkgo.GinkgoT()
		storageDir, _, _, service, user := setupTestAPIKeyService(t)
		userID := newFixtureUserID(user.ID)
		created, err := service.CreateAPIKey(userID, "MCP client", userID)
		if err != nil {
			t.Fatalf("CreateAPIKey failed: %v", err)
		}

		blocker, err := sql.Open("sqlite", filepath.Join(storageDir, "api_keys.db"))
		if err != nil {
			t.Fatalf("open blocking connection: %v", err)
		}
		defer blocker.Close()
		if _, err := blocker.Exec("BEGIN IMMEDIATE"); err != nil {
			t.Fatalf("begin blocking transaction: %v", err)
		}

		errs := make(chan error, 1)
		go func() {
			verified, err := service.VerifyAPIKey(created.Secret)
			if err != nil {
				errs <- err
				return
			}
			if verified.User.ID != user.ID {
				errs <- errors.New("verified wrong user")
				return
			}
			errs <- nil
		}()

		time.Sleep(100 * time.Millisecond)
		if _, err := blocker.Exec("ROLLBACK"); err != nil {
			t.Fatalf("rollback blocking transaction: %v", err)
		}

		select {
		case err := <-errs:
			if err != nil {
				t.Fatalf("VerifyAPIKey returned %v, want retry through transient last_used_at lock", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("VerifyAPIKey did not return after transient last_used_at lock was released")
		}
	})

	ginkgo.It("TestAPIKeyServiceVerifyRetriesTransientAPIKeyLookupLock", func() {
		t := ginkgo.GinkgoT()
		storageDir, _, _, service, user := setupTestAPIKeyService(t)
		userID := newFixtureUserID(user.ID)
		created, err := service.CreateAPIKey(userID, "MCP client", userID)
		if err != nil {
			t.Fatalf("CreateAPIKey failed: %v", err)
		}

		blocker := beginExclusiveSQLiteTransaction(t, filepath.Join(storageDir, "api_keys.db"))
		errs := make(chan error, 1)
		go func() {
			verified, err := service.VerifyAPIKey(created.Secret)
			if err != nil {
				errs <- err
				return
			}
			if verified.User.ID != user.ID {
				errs <- errors.New("verified wrong user")
				return
			}
			errs <- nil
		}()

		time.Sleep(100 * time.Millisecond)
		blocker.rollback(t)

		select {
		case err := <-errs:
			if err != nil {
				t.Fatalf("VerifyAPIKey returned %v, want retry through transient api key lookup lock", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("VerifyAPIKey did not return after transient api key lookup lock was released")
		}
	})

	ginkgo.It("TestAPIKeyServiceVerifyRetriesTransientUserLookupLock", func() {
		t := ginkgo.GinkgoT()
		storageDir, _, _, service, user := setupTestAPIKeyService(t)
		userID := newFixtureUserID(user.ID)
		created, err := service.CreateAPIKey(userID, "MCP client", userID)
		if err != nil {
			t.Fatalf("CreateAPIKey failed: %v", err)
		}

		blocker := beginExclusiveSQLiteTransaction(t, filepath.Join(storageDir, "users.db"))
		errs := make(chan error, 1)
		go func() {
			verified, err := service.VerifyAPIKey(created.Secret)
			if err != nil {
				errs <- err
				return
			}
			if verified.User.ID != user.ID {
				errs <- errors.New("verified wrong user")
				return
			}
			errs <- nil
		}()

		time.Sleep(100 * time.Millisecond)
		blocker.rollback(t)

		select {
		case err := <-errs:
			if err != nil {
				t.Fatalf("VerifyAPIKey returned %v, want retry through transient user lookup lock", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("VerifyAPIKey did not return after transient user lookup lock was released")
		}
	})

	ginkgo.It("TestAPIKeyStoreRevocationIsScopedToUser", func() {
		t := ginkgo.GinkgoT()
		_, userService, _, service, user := setupTestAPIKeyService(t)
		userID := newFixtureUserID(user.ID)
		other, err := userService.CreateUser("other", "other@example.com", "password123", RoleEditor)
		if err != nil {
			t.Fatalf("CreateUser other failed: %v", err)
		}

		created, err := service.CreateAPIKey(newFixtureUserID(other.ID), "Other key", userID)
		if err != nil {
			t.Fatalf("CreateAPIKey failed: %v", err)
		}

		if err := service.RevokeAPIKey(userID, created.Key.ID); !errors.Is(err, sql.ErrNoRows) && !errors.Is(err, ErrAPIKeyNotFound) {
			t.Fatalf("wrong-user RevokeAPIKey err = %v, want not found", err)
		}
		if _, err := service.VerifyAPIKey(created.Secret); err != nil {
			t.Fatalf("wrong-user revoke should not revoke key: %v", err)
		}
	})

	ginkgo.It("TestAPIKeyStoreMarkUsedRejectsRevokedKey", func() {
		t := ginkgo.GinkgoT()
		_, _, store, service, user := setupTestAPIKeyService(t)
		userID := newFixtureUserID(user.ID)

		created, err := service.CreateAPIKey(userID, "Race key", userID)
		if err != nil {
			t.Fatalf("CreateAPIKey failed: %v", err)
		}
		if err := service.RevokeAPIKey(userID, created.Key.ID); err != nil {
			t.Fatalf("RevokeAPIKey failed: %v", err)
		}

		if err := store.MarkAPIKeyUsed(created.Key.ID, service.now()); !errors.Is(err, ErrAPIKeyNotFound) {
			t.Fatalf("MarkAPIKeyUsed revoked key err = %v, want ErrAPIKeyNotFound", err)
		}
	})
})

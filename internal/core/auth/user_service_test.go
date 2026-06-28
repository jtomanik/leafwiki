package auth

import (
	ginkgo "github.com/onsi/ginkgo/v2"
)

func setupTestUserService(t authTestT) *UserService {
	t.Helper()
	store, err := NewUserStore(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to setup user store: %v", err)
	}
	return NewUserService(store)
}

var _ = ginkgo.Describe("user service", func() {
	ginkgo.It("TestUserService_CreateUser", func() {
		t := ginkgo.GinkgoT()
		service := setupTestUserService(t)

		user, err := service.CreateUser("alice", "alice@example.com", "secure", "admin")
		if err != nil {
			t.Fatalf("CreateUser failed: %v", err)
		}

		if user.Username != "alice" || user.Email != "alice@example.com" || user.Role != "admin" {
			t.Errorf("User not created with correct data")
		}
	})

	ginkgo.It("TestUserService_CreateUser_Duplicate", func() {
		t := ginkgo.GinkgoT()
		service := setupTestUserService(t)

		_, _ = service.CreateUser("alice", "alice@example.com", "secure", "editor")

		_, err := service.CreateUser("alice", "alice2@example.com", "secure", "editor")
		if err != ErrUserAlreadyExists {
			t.Errorf("Expected ErrUserAlreadyExists for username, got: %v", err)
		}

		_, err = service.CreateUser("bob", "alice@example.com", "secure", "editor")
		if err != ErrUserAlreadyExists {
			t.Errorf("Expected ErrUserAlreadyExists for email, got: %v", err)
		}
	})

	ginkgo.It("TestUserService_CreateUser_InvalidRole", func() {
		t := ginkgo.GinkgoT()
		service := setupTestUserService(t)

		_, err := service.CreateUser("bob", "bob@example.com", "secure", "guest")
		if err != ErrUserInvalidRole {
			t.Errorf("Expected ErrUserInvalidRole, got: %v", err)
		}
	})

	ginkgo.It("TestUserService_GetUserByEmailOrUsernameAndPassword", func() {
		t := ginkgo.GinkgoT()
		service := setupTestUserService(t)
		_, _ = service.CreateUser("alice", "alice@example.com", "mypassword", "editor")

		_, err := service.GetUserByEmailOrUsernameAndPassword("alice", "mypassword")
		if err != nil {
			t.Errorf("Valid login failed: %v", err)
		}

		_, err = service.GetUserByEmailOrUsernameAndPassword("alice@example.com", "mypassword")
		if err != nil {
			t.Errorf("Valid login by email failed: %v", err)
		}

		_, err = service.GetUserByEmailOrUsernameAndPassword("alice", "wrongpass")
		if err != ErrUserInvalidCredentials {
			t.Errorf("Expected ErrUserInvalidCredentials, got: %v", err)
		}
	})

	ginkgo.It("TestUserService_UpdateUser", func() {
		t := ginkgo.GinkgoT()
		service := setupTestUserService(t)

		user, _ := service.CreateUser("bob", "bob@example.com", "initial", "editor")
		userID := newFixtureUserID(user.ID)

		updated, err := service.UpdateUser(userID, "bobnew", "bobnew@example.com", "newpass", "admin")
		if err != nil {
			t.Fatalf("UpdateUser failed: %v", err)
		}

		if updated.Username != "bobnew" || updated.Email != "bobnew@example.com" || updated.Role != "admin" {
			t.Errorf("Update did not persist values")
		}
	})

	ginkgo.It("TestUserService_UpdateUser_EmptyRolePreservesExistingRole", func() {
		t := ginkgo.GinkgoT()
		service := setupTestUserService(t)

		user, _ := service.CreateUser("bob", "bob@example.com", "initial", RoleEditor)
		userID := newFixtureUserID(user.ID)

		updated, err := service.UpdateUser(userID, "bobnew", "bobnew@example.com", "", "")
		if err != nil {
			t.Fatalf("UpdateUser failed: %v", err)
		}

		if updated.Username != "bobnew" || updated.Email != "bobnew@example.com" {
			t.Errorf("Update did not persist profile fields")
		}
		if updated.Role != RoleEditor {
			t.Errorf("expected role %q, got %q", RoleEditor, updated.Role)
		}
	})
	ginkgo.It("TestUserService_UpdateUser_LastAdminCannotBeDemoted", func() {
		t := ginkgo.GinkgoT()
		service := setupTestUserService(t)

		admin, _ := service.CreateUser("admin", "admin@example.com", "pass", RoleAdmin)

		_, err := service.UpdateUser(newFixtureUserID(admin.ID), admin.Username, admin.Email, "", RoleViewer)
		if err != ErrLastAdminCannotBeDemoted {
			t.Errorf("expected ErrLastAdminCannotBeDemoted, got: %v", err)
		}
	})

	ginkgo.It("TestUserService_UpdateUser_AdminCanBeDemotedWhenAnotherAdminExists", func() {
		t := ginkgo.GinkgoT()
		service := setupTestUserService(t)

		admin1, _ := service.CreateUser("admin1", "admin1@example.com", "pass", RoleAdmin)
		_, _ = service.CreateUser("admin2", "admin2@example.com", "pass", RoleAdmin)

		updated, err := service.UpdateUser(newFixtureUserID(admin1.ID), admin1.Username, admin1.Email, "", RoleViewer)
		if err != nil {
			t.Fatalf("expected demotion to succeed with two admins, got: %v", err)
		}
		if updated.Role != RoleViewer {
			t.Errorf("expected role %q, got %q", RoleViewer, updated.Role)
		}
	})

	ginkgo.It("TestUserService_DeleteUser", func() {
		t := ginkgo.GinkgoT()
		service := setupTestUserService(t)

		// admin should not be deletable
		admin, _ := service.CreateUser("admin", "admin@example.com", "secret", "admin")
		err := service.DeleteUser(newFixtureUserID(admin.ID))
		if err != ErrUserAdminCannotBeDeleted {
			t.Errorf("Expected ErrUserAdminCannotBeDeleted when deleting admin, got: %v", err)
		}

		editor, _ := service.CreateUser("editor", "editor@example.com", "secret", "editor")
		err = service.DeleteUser(newFixtureUserID(editor.ID))
		if err != nil {
			t.Errorf("Failed to delete editor: %v", err)
		}
	})

	ginkgo.It("TestUserService_InitDefaultAdmin", func() {
		t := ginkgo.GinkgoT()
		store, _ := NewUserStore(t.TempDir())
		service := NewUserService(store)

		err := service.InitDefaultAdmin("")
		if err != nil {
			t.Errorf("InitDefaultAdmin failed: %v", err)
		}

		users, err := service.GetUsers()
		if err != nil || len(users) != 1 || users[0].Username != "admin" {
			t.Errorf("Expected default admin user, got: %+v", users)
		}
	})

	ginkgo.It("TestUserService_ResetAdminUserPassword", func() {
		t := ginkgo.GinkgoT()
		service := setupTestUserService(t)
		defer closeWithErrorCheck(service.Close)

		// Create initial admin user
		_, err := service.CreateUser("admin", "admin@example.com", "oldpassword", "admin")
		if err != nil {
			t.Fatalf("Failed to create admin user: %v", err)
		}

		// Reset admin password
		adminUser, err := service.ResetAdminUserPassword()
		if err != nil {
			t.Fatalf("ResetAdminUserPassword failed: %v", err)
		}

		if adminUser.Username != "admin" {
			t.Errorf("Expected username 'admin', got: %s", adminUser.Username)
		}

		if adminUser.Password == "" {
			t.Errorf("Expected a new password to be generated, got empty string")
		}

		if adminUser.Password == "oldpassword" {
			t.Errorf("Expected password to be different from old password")
		}

		// Verify we can log in with the new password
		_, err = service.GetUserByEmailOrUsernameAndPassword("admin", adminUser.Password)
		if err != nil {
			t.Errorf("Failed to login with new password: %v", err)
		}

		// Verify old password no longer works
		_, err = service.GetUserByEmailOrUsernameAndPassword("admin", "oldpassword")
		if err != ErrUserInvalidCredentials {
			t.Errorf("Expected ErrUserInvalidCredentials for old password, got: %v", err)
		}
	})

	ginkgo.It("TestUserService_ResetAdminUserPassword_NoAdmin", func() {
		t := ginkgo.GinkgoT()
		service := setupTestUserService(t)
		defer closeWithErrorCheck(service.Close)

		// Don't create an admin user first - test should create one

		// Reset admin password (should create new admin)
		adminUser, err := service.ResetAdminUserPassword()
		if err != nil {
			t.Fatalf("ResetAdminUserPassword failed: %v", err)
		}

		if adminUser.Username != "admin" {
			t.Errorf("Expected username 'admin', got: %s", adminUser.Username)
		}

		if adminUser.Email != "admin@localhost" {
			t.Errorf("Expected email 'admin@localhost', got: %s", adminUser.Email)
		}

		if adminUser.Password == "" {
			t.Errorf("Expected a new password to be generated, got empty string")
		}

		// Verify we can log in with the new password
		_, err = service.GetUserByEmailOrUsernameAndPassword("admin", adminUser.Password)
		if err != nil {
			t.Errorf("Failed to login with new password: %v", err)
		}
	})
})

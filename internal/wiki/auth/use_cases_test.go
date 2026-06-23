package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"

	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

func setupUpdateUserUseCase(t *testing.T) (*UpdateUserUseCase, *coreauth.UserService) {
	t.Helper()
	store, err := coreauth.NewUserStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewUserStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	userSvc := coreauth.NewUserService(store)
	resolver, err := coreauth.NewUserResolver(userSvc)
	if err != nil {
		t.Fatalf("NewUserResolver: %v", err)
	}
	return NewUpdateUserUseCase(userSvc, resolver, slog.Default()), userSvc
}

func TestUpdateUser_AdminCanChangeRole(t *testing.T) {
	uc, svc := setupUpdateUserUseCase(t)

	viewer, err := svc.CreateUser("viewer", "viewer@example.com", "pass", coreauth.RoleViewer)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	out, err := uc.Execute(context.Background(), UpdateUserInput{
		ID:               coreauth.NewUserIDUnchecked(viewer.ID),
		Username:         viewer.Username,
		Email:            viewer.Email,
		Role:             coreauth.RoleAdmin,
		RequesterIsAdmin: true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.User.Role != coreauth.RoleAdmin {
		t.Errorf("expected role %q, got %q", coreauth.RoleAdmin, out.User.Role)
	}
}

func TestUpdateUser_AdminCanUpdateProfileWithoutRole(t *testing.T) {
	uc, svc := setupUpdateUserUseCase(t)

	editor, err := svc.CreateUser("ed", "ed@example.com", "pass", coreauth.RoleEditor)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	out, err := uc.Execute(context.Background(), UpdateUserInput{
		ID:               coreauth.NewUserIDUnchecked(editor.ID),
		Username:         "ed-admin-updated",
		Email:            "ed-admin-updated@example.com",
		Role:             "",
		RequesterIsAdmin: true,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.User.Username != "ed-admin-updated" {
		t.Errorf("expected username %q, got %q", "ed-admin-updated", out.User.Username)
	}
	if out.User.Email != "ed-admin-updated@example.com" {
		t.Errorf("expected email %q, got %q", "ed-admin-updated@example.com", out.User.Email)
	}
	if out.User.Role != coreauth.RoleEditor {
		t.Errorf("expected role %q, got %q", coreauth.RoleEditor, out.User.Role)
	}
}

func TestUpdateUser_NonAdminCannotEscalateRole(t *testing.T) {
	uc, svc := setupUpdateUserUseCase(t)

	viewer, err := svc.CreateUser("viewer", "viewer@example.com", "pass", coreauth.RoleViewer)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	out, err := uc.Execute(context.Background(), UpdateUserInput{
		ID:               coreauth.NewUserIDUnchecked(viewer.ID),
		Username:         viewer.Username,
		Email:            viewer.Email,
		Role:             coreauth.RoleAdmin,
		RequesterIsAdmin: false,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.User.Role != coreauth.RoleViewer {
		t.Errorf("role escalation succeeded: expected %q, got %q", coreauth.RoleViewer, out.User.Role)
	}
}

func TestUpdateUser_NonAdminCanUpdateOwnProfile(t *testing.T) {
	uc, svc := setupUpdateUserUseCase(t)

	editor, err := svc.CreateUser("ed", "ed@example.com", "pass", coreauth.RoleEditor)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	out, err := uc.Execute(context.Background(), UpdateUserInput{
		ID:               coreauth.NewUserIDUnchecked(editor.ID),
		Username:         "ed-updated",
		Email:            "ed-updated@example.com",
		Role:             coreauth.RoleAdmin,
		RequesterIsAdmin: false,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.User.Username != "ed-updated" {
		t.Errorf("expected username %q, got %q", "ed-updated", out.User.Username)
	}
	if out.User.Email != "ed-updated@example.com" {
		t.Errorf("expected email %q, got %q", "ed-updated@example.com", out.User.Email)
	}
	if out.User.Role != coreauth.RoleEditor {
		t.Errorf("role must not change: expected %q, got %q", coreauth.RoleEditor, out.User.Role)
	}
}

func TestUpdateUser_LastAdminCannotSelfDemote(t *testing.T) {
	uc, svc := setupUpdateUserUseCase(t)

	admin, err := svc.CreateUser("admin", "admin@example.com", "pass", coreauth.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	_, err = uc.Execute(context.Background(), UpdateUserInput{
		ID:               coreauth.NewUserIDUnchecked(admin.ID),
		Username:         admin.Username,
		Email:            admin.Email,
		Role:             coreauth.RoleViewer,
		RequesterIsAdmin: true,
	})
	if !errors.Is(err, coreauth.ErrLastAdminCannotBeDemoted) {
		t.Errorf("expected ErrLastAdminCannotBeDemoted, got: %v", err)
	}
}

func TestUpdateUser_AdminCanBeDemotedWhenAnotherExists(t *testing.T) {
	uc, svc := setupUpdateUserUseCase(t)

	admin1, err := svc.CreateUser("admin1", "admin1@example.com", "pass", coreauth.RoleAdmin)
	if err != nil {
		t.Fatalf("CreateUser admin1: %v", err)
	}
	if _, err := svc.CreateUser("admin2", "admin2@example.com", "pass", coreauth.RoleAdmin); err != nil {
		t.Fatalf("CreateUser admin2: %v", err)
	}

	out, err := uc.Execute(context.Background(), UpdateUserInput{
		ID:               coreauth.NewUserIDUnchecked(admin1.ID),
		Username:         admin1.Username,
		Email:            admin1.Email,
		Role:             coreauth.RoleViewer,
		RequesterIsAdmin: true,
	})
	if err != nil {
		t.Fatalf("expected demotion to succeed, got: %v", err)
	}
	if out.User.Role != coreauth.RoleViewer {
		t.Errorf("expected role %q, got %q", coreauth.RoleViewer, out.User.Role)
	}
}

func TestUpdateUser_AdminInvalidRole(t *testing.T) {
	uc, svc := setupUpdateUserUseCase(t)

	user, err := svc.CreateUser("alice", "alice@example.com", "pass", coreauth.RoleViewer)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	_, err = uc.Execute(context.Background(), UpdateUserInput{
		ID:               coreauth.NewUserIDUnchecked(user.ID),
		Username:         user.Username,
		Email:            user.Email,
		Role:             "superuser",
		RequesterIsAdmin: true,
	})
	if err == nil {
		t.Fatal("expected validation error for invalid role, got nil")
	}
}

func TestCreateUserUseCaseValidationReturnsStableFieldCodes(t *testing.T) {
	uc := NewCreateUserUseCase(nil, nil, slog.Default())

	_, err := uc.Execute(context.Background(), CreateUserInput{
		Email:    "not-an-email",
		Password: "short",
		Role:     "invalid",
	})

	var ve *sharederrors.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("error = %T %v, want ValidationErrors", err, err)
	}
	assertAuthFieldErrorCode(t, ve, "username", "auth_username_required", "validation.auth.username_required")
	assertAuthFieldErrorCode(t, ve, "email", "auth_email_invalid", "validation.auth.email_invalid")
	assertAuthFieldErrorCode(t, ve, "password", "auth_password_too_short", "validation.auth.password_too_short")
	assertAuthFieldErrorCode(t, ve, "role", "auth_role_invalid", "validation.auth.role_invalid")
}

func TestCreateAPIKeyUseCaseValidationReturnsStableFieldCodes(t *testing.T) {
	uc := NewCreateAPIKeyUseCase(nil, nil)

	_, err := uc.Execute(context.Background(), CreateAPIKeyInput{Name: ""})

	var ve *sharederrors.ValidationErrors
	if !errors.As(err, &ve) {
		t.Fatalf("error = %T %v, want ValidationErrors", err, err)
	}
	assertAuthFieldErrorCode(t, ve, "name", "auth_api_key_name_required", "validation.auth.api_key_name_required")
}

func TestAPIKeyUseCaseInputsUseSemanticIDs(t *testing.T) {
	_ = GetUserByIDInput{ID: coreauth.NewUserIDUnchecked("user-1")}
	_ = CreateAPIKeyInput{
		UserID:          coreauth.NewUserIDUnchecked("user-1"),
		CreatedByUserID: coreauth.NewUserIDUnchecked("admin-1"),
	}
	_ = ListAPIKeysInput{UserID: coreauth.NewUserIDUnchecked("user-1")}
	_ = RevokeAPIKeyInput{UserID: coreauth.NewUserIDUnchecked("user-1"), KeyID: coreauth.NewAPIKeyIDUnchecked("key-1")}
}

func assertAuthFieldErrorCode(t *testing.T, ve *sharederrors.ValidationErrors, field string, code string, messageID string) {
	t.Helper()
	for _, got := range ve.Errors {
		if got.Field != field {
			continue
		}
		if fmt.Sprintf("%s", got.Code) != code {
			t.Fatalf("%s code = %q, want %q", field, got.Code, code)
		}
		if fmt.Sprintf("%s", got.MessageID) != messageID {
			t.Fatalf("%s messageId = %q, want %q", field, got.MessageID, messageID)
		}
		return
	}
	t.Fatalf("field %q not found in %#v", field, ve.Errors)
}

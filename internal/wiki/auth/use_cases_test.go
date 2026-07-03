package auth

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"

	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	testmatchers "github.com/perber/wiki/internal/test_utils/matchers"
)

func setupUpdateUserUseCase() (*UpdateUserUseCase, *coreauth.UserService) {
	ginkgo.GinkgoHelper()
	userSvc := setupUserService()
	resolver, err := coreauth.NewUserResolver(userSvc)
	Expect(err).NotTo(HaveOccurred())
	return NewUpdateUserUseCase(userSvc, resolver, slog.Default()), userSvc
}

func setupUserService() *coreauth.UserService {
	ginkgo.GinkgoHelper()
	store, err := coreauth.NewUserStore(tempAuthStorageDir())
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
	return coreauth.NewUserService(store)
}

func tempAuthStorageDir() string {
	ginkgo.GinkgoHelper()
	storageDir, err := os.MkdirTemp("", "leafwiki-auth-*")
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(os.RemoveAll, storageDir)
	return storageDir
}

func setupUserServiceWithUnusableStorageDir() *coreauth.UserService {
	ginkgo.GinkgoHelper()
	storageDir := tempAuthStorageDir()
	store, err := coreauth.NewUserStore(storageDir)
	Expect(err).NotTo(HaveOccurred())
	service := coreauth.NewUserService(store)

	Expect(service.Close()).To(Succeed())
	Expect(os.RemoveAll(storageDir)).To(Succeed())
	Expect(os.WriteFile(storageDir, []byte("not a directory"), 0o600)).To(Succeed())

	return service
}

func setupAPIKeyService(userSvc *coreauth.UserService) *coreauth.APIKeyService {
	ginkgo.GinkgoHelper()
	store, err := coreauth.NewAPIKeyStore(tempAuthStorageDir())
	Expect(err).NotTo(HaveOccurred())
	ginkgo.DeferCleanup(func() {
		Expect(store.Close()).To(Succeed())
	})
	return coreauth.NewAPIKeyService(store, userSvc)
}

var _ = ginkgo.Describe("auth use cases", func() {
	ginkgo.It("lets administrators assign a different role to a user", func() {
		uc, svc := setupUpdateUserUseCase()

		viewer, err := svc.CreateUser("viewer", "viewer@example.com", "pass", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())

		out, err := uc.Execute(context.Background(), UpdateUserInput{
			ID:               newFixtureUserID(viewer.ID),
			Username:         viewer.Username,
			Email:            viewer.Email,
			Role:             coreauth.RoleAdmin,
			RequesterIsAdmin: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.User.Role).To(Equal(coreauth.RoleAdmin))
	})

	ginkgo.It("updates profile details without changing role when administrators omit role", func() {
		uc, svc := setupUpdateUserUseCase()

		editor, err := svc.CreateUser("ed", "ed@example.com", "pass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())

		out, err := uc.Execute(context.Background(), UpdateUserInput{
			ID:               newFixtureUserID(editor.ID),
			Username:         "ed-admin-updated",
			Email:            "ed-admin-updated@example.com",
			Role:             "",
			RequesterIsAdmin: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.User).To(MatchAuthPublicUser(gstruct.Fields{
			"Username": Equal("ed-admin-updated"),
			"Email":    Equal("ed-admin-updated@example.com"),
			"Role":     Equal(coreauth.RoleEditor),
		}))
	})

	ginkgo.It("preserves the current role when a non-admin requester asks for escalation", func() {
		uc, svc := setupUpdateUserUseCase()

		viewer, err := svc.CreateUser("viewer", "viewer@example.com", "pass", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())

		out, err := uc.Execute(context.Background(), UpdateUserInput{
			ID:               newFixtureUserID(viewer.ID),
			Username:         viewer.Username,
			Email:            viewer.Email,
			Role:             coreauth.RoleAdmin,
			RequesterIsAdmin: false,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.User.Role).To(Equal(coreauth.RoleViewer))
	})

	ginkgo.It("lets non-admin users update their profile without escalating role", func() {
		uc, svc := setupUpdateUserUseCase()

		editor, err := svc.CreateUser("ed", "ed@example.com", "pass", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())

		out, err := uc.Execute(context.Background(), UpdateUserInput{
			ID:               newFixtureUserID(editor.ID),
			Username:         "ed-updated",
			Email:            "ed-updated@example.com",
			Role:             coreauth.RoleAdmin,
			RequesterIsAdmin: false,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.User).To(MatchAuthPublicUser(gstruct.Fields{
			"Username": Equal("ed-updated"),
			"Email":    Equal("ed-updated@example.com"),
			"Role":     Equal(coreauth.RoleEditor),
		}))
	})

	ginkgo.It("rejects demoting the only administrator", func() {
		uc, svc := setupUpdateUserUseCase()

		admin, err := svc.CreateUser("admin", "admin@example.com", "pass", coreauth.RoleAdmin)
		Expect(err).NotTo(HaveOccurred())

		_, err = uc.Execute(context.Background(), UpdateUserInput{
			ID:               newFixtureUserID(admin.ID),
			Username:         admin.Username,
			Email:            admin.Email,
			Role:             coreauth.RoleViewer,
			RequesterIsAdmin: true,
		})
		Expect(err).To(MatchError(coreauth.ErrLastAdminCannotBeDemoted))
	})

	ginkgo.It("demotes an administrator when another administrator remains", func() {
		uc, svc := setupUpdateUserUseCase()

		admin1, err := svc.CreateUser("admin1", "admin1@example.com", "pass", coreauth.RoleAdmin)
		Expect(err).NotTo(HaveOccurred())
		_, err = svc.CreateUser("admin2", "admin2@example.com", "pass", coreauth.RoleAdmin)
		Expect(err).NotTo(HaveOccurred())

		out, err := uc.Execute(context.Background(), UpdateUserInput{
			ID:               newFixtureUserID(admin1.ID),
			Username:         admin1.Username,
			Email:            admin1.Email,
			Role:             coreauth.RoleViewer,
			RequesterIsAdmin: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.User.Role).To(Equal(coreauth.RoleViewer))
	})

	ginkgo.It("rejects unsupported roles before updating a user", func() {
		uc, svc := setupUpdateUserUseCase()

		user, err := svc.CreateUser("alice", "alice@example.com", "pass", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())

		_, err = uc.Execute(context.Background(), UpdateUserInput{
			ID:               newFixtureUserID(user.ID),
			Username:         user.Username,
			Email:            user.Email,
			Role:             "superuser",
			RequesterIsAdmin: true,
		})
		Expect(err).To(HaveOccurred())
	})

	ginkgo.It("returns stable localized field codes for invalid user creation input", func() {
		uc := NewCreateUserUseCase(nil, nil, slog.Default())

		_, err := uc.Execute(context.Background(), CreateUserInput{
			Email:    "not-an-email",
			Password: "short",
			Role:     "invalid",
		})

		Expect(err).To(SatisfyAll(
			HaveAuthFieldErrorCode("username", FieldCodeAuthUsernameRequired, MessageIDAuthUsernameRequired),
			HaveAuthFieldErrorCode("email", FieldCodeAuthEmailInvalid, MessageIDAuthEmailInvalid),
			HaveAuthFieldErrorCode("password", FieldCodeAuthPasswordTooShort, MessageIDAuthPasswordTooShort),
			HaveAuthFieldErrorCode("role", FieldCodeAuthRoleInvalid, MessageIDAuthRoleInvalid),
		))
	})

	ginkgo.It("returns stable localized field codes for missing API key names", func() {
		uc := NewCreateAPIKeyUseCase(nil, nil)

		_, err := uc.Execute(context.Background(), CreateAPIKeyInput{Name: ""})

		Expect(err).To(HaveAuthFieldErrorCode("name", FieldCodeAuthAPIKeyNameRequired, MessageIDAuthAPIKeyNameRequired))
	})

	ginkgo.It("requires semantic user and API key identifiers in use-case inputs", func() {
		_ = GetUserByIDInput{ID: newFixtureUserID("user-1")}
		_ = CreateAPIKeyInput{
			UserID:          newFixtureUserID("user-1"),
			CreatedByUserID: newFixtureUserID("admin-1"),
		}
		_ = ListAPIKeysInput{UserID: newFixtureUserID("user-1")}
		_ = RevokeAPIKeyInput{UserID: newFixtureUserID("user-1"), KeyID: coreauth.APIKeyID("key-1")}
	})

		ginkgo.It("returns auth-disabled errors for login, logout, and refresh when no auth service is configured", func() {
		_, err := NewLoginUseCase(nil).Execute(context.Background(), LoginInput{Identifier: "admin", Password: "password"})
		Expect(err).To(MatchError(ErrAuthDisabled))

		err = NewLogoutUseCase(nil).Execute(context.Background(), LogoutInput{RefreshToken: "refresh"})
		Expect(err).To(MatchError(ErrAuthDisabled))

		_, err = NewRefreshTokenUseCase(nil).Execute(context.Background(), RefreshTokenInput{RefreshToken: "refresh"})
		Expect(err).To(MatchError(ErrAuthDisabled))
	})

	ginkgo.It("GetUsersUseCase and GetUserByIDUseCase return public users", func() {
		userSvc := setupUserService()
		admin, err := userSvc.CreateUser("admin", "admin@example.com", "password123", coreauth.RoleAdmin)
		Expect(err).NotTo(HaveOccurred())
		viewer, err := userSvc.CreateUser("viewer", "viewer@example.com", "password123", coreauth.RoleViewer)
		Expect(err).NotTo(HaveOccurred())

		usersOut, err := NewGetUsersUseCase(userSvc).Execute(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(usersOut.Users).To(ConsistOf(
			SatisfyAll(HaveField("ID", admin.ID), HaveField("Username", "admin"), HaveField("Role", coreauth.RoleAdmin)),
			SatisfyAll(HaveField("ID", viewer.ID), HaveField("Username", "viewer"), HaveField("Role", coreauth.RoleViewer)),
		))

		userOut, err := NewGetUserByIDUseCase(userSvc).Execute(context.Background(), GetUserByIDInput{ID: newFixtureUserID(viewer.ID)})
		Expect(err).NotTo(HaveOccurred())
		Expect(userOut.User).To(MatchAuthPublicUser(gstruct.Fields{
			"ID":       Equal(viewer.ID),
			"Username": Equal("viewer"),
		}))
	})

	ginkgo.It("GetUsersUseCase returns storage errors", func() {
		userSvc := setupUserServiceWithUnusableStorageDir()

		_, err := NewGetUsersUseCase(userSvc).Execute(context.Background())

		Expect(err).To(HaveOccurred())
	})

	ginkgo.It("ChangeOwnPasswordUseCase validates the old password and updates a matching user password", func() {
		userSvc := setupUserService()
		user, err := userSvc.CreateUser("alice", "alice@example.com", "old-password", coreauth.RoleEditor)
		Expect(err).NotTo(HaveOccurred())
		uc := NewChangeOwnPasswordUseCase(userSvc)

		err = uc.Execute(context.Background(), ChangeOwnPasswordInput{
			UserID:      newFixtureUserID(user.ID),
			OldPassword: "wrong-password",
			NewPassword: "new-password",
		})
		Expect(err).To(HaveAuthFieldErrorCode("oldPassword", FieldCodeAuthOldPasswordIncorrect, MessageIDAuthOldPasswordIncorrect))

		err = uc.Execute(context.Background(), ChangeOwnPasswordInput{
			UserID:      newFixtureUserID(user.ID),
			OldPassword: "old-password",
			NewPassword: "new-password",
		})
		Expect(err).NotTo(HaveOccurred())
		_, err = userSvc.DoesIDAndPasswordMatch(newFixtureUserID(user.ID), "old-password")
		Expect(err).To(MatchError(coreauth.ErrUserInvalidCredentials))
		_, err = userSvc.DoesIDAndPasswordMatch(newFixtureUserID(user.ID), "new-password")
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.It("user mutation use cases continue when resolver reload only logs a warning", func() {
		userSvc := setupUserService()
		resolver := failingUserResolverReloader{err: errors.New("reload failed")}

		createOut, err := (&CreateUserUseCase{
			user:     userSvc,
			resolver: resolver,
			log:      slog.Default(),
		}).Execute(context.Background(), CreateUserInput{
			Username: "reload-user",
			Email:    "reload@example.com",
			Password: "password123",
			Role:     coreauth.RoleEditor,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(createOut.User.Username).To(Equal("reload-user"))

		updateOut, err := (&UpdateUserUseCase{
			user:     userSvc,
			resolver: resolver,
			log:      slog.Default(),
		}).Execute(context.Background(), UpdateUserInput{
			ID:               newFixtureUserID(createOut.User.ID),
			Username:         "reload-user-updated",
			Email:            "reload-updated@example.com",
			Role:             coreauth.RoleViewer,
			RequesterIsAdmin: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(updateOut.User).To(MatchAuthPublicUser(gstruct.Fields{
			"Username": Equal("reload-user-updated"),
			"Role":     Equal(coreauth.RoleViewer),
		}))

		err = (&DeleteUserUseCase{
			user:     userSvc,
			resolver: resolver,
			log:      slog.Default(),
		}).Execute(context.Background(), DeleteUserInput{ID: newFixtureUserID(createOut.User.ID)})
		Expect(err).NotTo(HaveOccurred())
		_, err = userSvc.GetUserByID(newFixtureUserID(createOut.User.ID))
		Expect(err).To(MatchError(coreauth.ErrUserNotFound))
	})

	ginkgo.It("CreateAPIKeyUseCase validates current password when required", func() {
		userSvc := setupUserService()
		apiKeys := setupAPIKeyService(userSvc)
		user, err := userSvc.CreateUser("admin", "admin@example.com", "password123", coreauth.RoleAdmin)
		Expect(err).NotTo(HaveOccurred())
		uc := NewCreateAPIKeyUseCase(apiKeys, userSvc)

		_, err = uc.Execute(context.Background(), CreateAPIKeyInput{
			UserID:                 newFixtureUserID(user.ID),
			Name:                   "key",
			CreatedByUserID:        newFixtureUserID(user.ID),
			CurrentPassword:        "wrong",
			RequireCurrentPassword: true,
		})
		Expect(err).To(HaveAuthFieldErrorCode("currentPassword", FieldCodeAuthCurrentPasswordIncorrect, MessageIDAuthCurrentPasswordIncorrect))

		out, err := uc.Execute(context.Background(), CreateAPIKeyInput{
			UserID:                 newFixtureUserID(user.ID),
			Name:                   " key ",
			CreatedByUserID:        newFixtureUserID(user.ID),
			CurrentPassword:        "password123",
			RequireCurrentPassword: true,
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.Secret).To(HavePrefix(coreauth.APIKeyPrefix))
		Expect(out.Key.Name).To(Equal("key"))
	})

	ginkgo.It("ListAPIKeysUseCase and RevokeAPIKeyUseCase round-trip active keys", func() {
		userSvc := setupUserService()
		apiKeys := setupAPIKeyService(userSvc)
		user, err := userSvc.CreateUser("admin", "admin@example.com", "password123", coreauth.RoleAdmin)
		Expect(err).NotTo(HaveOccurred())
		created, err := apiKeys.CreateAPIKey(newFixtureUserID(user.ID), "key", newFixtureUserID(user.ID))
		Expect(err).NotTo(HaveOccurred())

		listed, err := NewListAPIKeysUseCase(apiKeys).Execute(context.Background(), ListAPIKeysInput{UserID: newFixtureUserID(user.ID)})
		Expect(err).NotTo(HaveOccurred())
		Expect(listed.Keys).To(ConsistOf(HaveField("ID", Equal(created.Key.ID))))

		err = NewRevokeAPIKeyUseCase(apiKeys).Execute(context.Background(), RevokeAPIKeyInput{
			UserID: newFixtureUserID(user.ID),
			KeyID:  created.Key.ID,
		})
		Expect(err).NotTo(HaveOccurred())
		listed, err = NewListAPIKeysUseCase(apiKeys).Execute(context.Background(), ListAPIKeysInput{UserID: newFixtureUserID(user.ID)})
		Expect(err).NotTo(HaveOccurred())
		Expect(listed.Keys).To(BeEmpty())
	})

	ginkgo.It("auth error helpers map localized codes and success messages", func() {
		Expect(authErrorStatus(ErrCodeAuthUserNotFound)).To(Equal(404))
		Expect(authErrorStatus(ErrCodeAuthForbidden)).To(Equal(403))
		Expect(authErrorStatus("unknown")).To(Equal(500))
		Expect(apiSuccessMessage(MessageIDAuthLoginSuccess)).NotTo(BeEmpty())
	})

		ginkgo.It("delegates login, refresh, and logout operations to the configured auth service", func() {
		userSvc := setupUserService()
		sessionStore, err := coreauth.NewSessionStore(tempAuthStorageDir())
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(func() {
			Expect(sessionStore.Close()).To(Succeed())
		})
		_, err = userSvc.CreateUser("admin", "admin@example.com", "password123", coreauth.RoleAdmin)
		Expect(err).NotTo(HaveOccurred())
		authSvc := coreauth.NewAuthService(userSvc, sessionStore, strings.Repeat("s", 32), time.Minute, time.Hour)

		loginOut, err := NewLoginUseCase(authSvc).Execute(context.Background(), LoginInput{
			Identifier: "admin",
			Password:   "password123",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(loginOut.Token.User.Username).To(Equal("admin"))

		refreshOut, err := NewRefreshTokenUseCase(authSvc).Execute(context.Background(), RefreshTokenInput{RefreshToken: loginOut.Token.RefreshToken})
		Expect(err).NotTo(HaveOccurred())
		Expect(refreshOut.Token.User.Username).To(Equal("admin"))

		err = NewLogoutUseCase(authSvc).Execute(context.Background(), LogoutInput{RefreshToken: refreshOut.Token.RefreshToken})
		Expect(err).NotTo(HaveOccurred())
	})
})

func HaveAuthFieldErrorCode(field testmatchers.ValidationField, code sharederrors.FieldErrorCode, messageID sharederrors.MessageID) types.GomegaMatcher {
	return WithTransform(func(err error) *sharederrors.ValidationErrors {
		var validation *sharederrors.ValidationErrors
		if !errors.As(err, &validation) {
			return nil
		}
		return validation
	}, testmatchers.ContainFieldError(field, code, messageID))
}

func MatchAuthPublicUser(fields gstruct.Fields) types.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, fields))
}

type failingUserResolverReloader struct {
	err error
}

func (r failingUserResolverReloader) Reload() error {
	return r.err
}

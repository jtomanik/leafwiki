package auth

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type UserService struct {
	store *UserStore
}

func NewUserService(store *UserStore) *UserService {
	return &UserService{
		store: store,
	}
}

func (s *UserService) InitDefaultAdmin(newPassword string) error {
	// Check if admin user already exists

	if _, err := authUserStoreGetAdminUser(s.store); err == nil {
		// Admin user already exists, no need to create a new one
		return nil
	}

	if _, err := s.CreateUser(DefaultAdminUsername, DefaultAdminEmail, newPassword, RoleAdmin); err != nil {
		return fmt.Errorf("failed to create default admin: %w", err)
	}

	return nil
}

func (s *UserService) CreateUser(username, email, password, role string) (*User, error) {
	// Check if user already exists
	_, err := authUserStoreGetUserByUsername(s.store, username)
	if err == nil {
		return nil, ErrUserAlreadyExists
	}

	// Check if email already exists
	_, err = authUserStoreGetUserByEmail(s.store, email)
	if err == nil {
		return nil, ErrUserAlreadyExists
	}

	// Validate role
	if !IsValidRole(role) {
		return nil, ErrUserInvalidRole
	}

	// hash password
	hashedPassword, err := authGeneratePasswordHash([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	// Generate unique ID
	id, err := authGenerateUniqueID()
	if err != nil {
		return nil, err
	}

	// Create new user
	user := &User{
		ID:       UserIDFromString(id),
		Username: username,
		Email:    email,
		Password: string(hashedPassword),
		Role:     role,
	}

	// Save user to store
	err = authUserStoreCreateUser(s.store, user)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (s *UserService) GetUserByID(id UserID) (*User, error) {
	user, err := authUserStoreGetUserByID(s.store, id)
	if err != nil {
		if !errors.Is(err, ErrUserNotFound) {
			return nil, err
		}
		return nil, ErrUserNotFound
	}

	return user, nil
}

func (s *UserService) UpdateUser(id UserID, username, email, password, role string) (*User, error) {
	// Check if user exists
	user, err := authUserStoreGetUserByID(s.store, id)
	if err != nil {
		return nil, ErrUserNotFound
	}

	// Check if username already exists (but if it's the same user, ignore)
	existingUser, err := authUserStoreGetUserByUsername(s.store, username)
	if err == nil && existingUser.ID != id {
		return nil, ErrUserAlreadyExists
	}

	// Check if email already exists (but if it's the same user, ignore)
	existingUser, err = authUserStoreGetUserByEmail(s.store, email)
	if err == nil && existingUser.ID != id {
		return nil, ErrUserAlreadyExists
	}

	if strings.TrimSpace(role) == "" {
		role = user.Role
	}

	// Validate role
	if !IsValidRole(role) {
		return nil, ErrUserInvalidRole
	}

	// Prevent demoting the last admin
	if user.HasRole(RoleAdmin) && role != RoleAdmin {
		count, err := authUserStoreCountAdminUsers(s.store)
		if err != nil {
			return nil, err
		}
		if count <= 1 {
			return nil, ErrLastAdminCannotBeDemoted
		}
	}

	// Update user fields
	user.Username = username
	user.Email = email
	user.Role = role

	if password != "" {
		hashedPassword, err := authGeneratePasswordHash([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		user.Password = string(hashedPassword)
	}

	// Save updated user to store
	err = authUserStoreUpdateUser(s.store, user)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func (s *UserService) UpdatePassword(id UserID, newpassword string) error {
	// Check if user exists
	_, err := authUserStoreGetUserByID(s.store, id)
	if err != nil {
		return err
	}

	// hash password
	hashedPassword, err := authGeneratePasswordHash([]byte(newpassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// Save updated user to store
	err = authUserStoreUpdatePassword(s.store, id, string(hashedPassword))
	if err != nil {
		return err
	}

	return nil
}

func (s *UserService) DoesIDAndPasswordMatch(id UserID, password string) (bool, error) {
	// Check if user exists
	user, err := authUserStoreGetUserByID(s.store, id)
	if err != nil {
		return false, ErrUserNotFound
	}

	// Check if password is correct
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		return false, ErrUserInvalidCredentials
	}

	return true, nil
}

func (s *UserService) DeleteUser(id UserID) error {
	// Check if user exists
	user, err := authUserStoreGetUserByID(s.store, id)
	if err != nil {
		return ErrUserNotFound
	}
	// Check if user is admin
	if user.HasRole(RoleAdmin) {
		return ErrUserAdminCannotBeDeleted
	}
	// Delete user from store
	err = authUserStoreDeleteUser(s.store, id)
	if err != nil {
		return err
	}
	return nil
}

func (s *UserService) GetUsers() ([]*User, error) {
	users, err := authUserStoreGetAllUsers(s.store)
	if err != nil {
		return nil, err
	}
	return users, nil
}

func (s *UserService) GetUserByUsername(username string) (*User, error) {
	user, err := authUserStoreGetUserByUsername(s.store, username)
	if err != nil {
		return nil, ErrUserNotFound
	}
	return user, nil
}

func (s *UserService) GetUserByIdentifier(identifier string) (*User, error) {
	user, err := authUserStoreGetUserByUsername(s.store, identifier)
	if err != nil {
		user, err = authUserStoreGetUserByEmail(s.store, identifier)
		if err != nil {
			return nil, ErrUserNotFound
		}
	}
	return user, nil
}

func (s *UserService) GetUserByEmailOrUsernameAndPassword(identifier, password string) (*User, error) {
	user, err := authUserStoreGetUserByUsername(s.store, identifier)
	if err != nil {
		user, err = authUserStoreGetUserByEmail(s.store, identifier)
		if err != nil {
			return nil, ErrUserNotFound
		}
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password))
	if err != nil {
		return nil, ErrUserInvalidCredentials
	}

	return user, nil
}

func (s *UserService) ChangeOwnPassword(id UserID, oldPassword, newPassword string) error {
	// Check if user exists
	user, err := authUserStoreGetUserByID(s.store, id)
	if err != nil {
		return ErrUserNotFound
	}

	// Check if old password is correct
	err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(oldPassword))
	if err != nil {
		return ErrUserInvalidCredentials
	}

	// hash new password
	hashedPassword, err := authGeneratePasswordHash([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// Save updated user to store
	err = authUserStoreUpdatePassword(s.store, id, string(hashedPassword))
	if err != nil {
		return err
	}

	return nil
}

func (s *UserService) ResetAdminUserPassword() (*User, error) {
	// Generate a new password for the admin user
	password, err := authGenerateRandomPassword(16)
	if err != nil {
		return nil, fmt.Errorf("failed to generate password: %w", err)
	}

	// if the user is not found create a new one
	adminUser, err := authUserStoreGetAdminUser(s.store)
	if err != nil {
		if err == ErrUserNotFound {
			// Create default admin user
			adminUser, err = s.CreateUser(DefaultAdminUsername, DefaultAdminEmail, password, RoleAdmin)
			if err != nil {
				return nil, fmt.Errorf("failed to create default admin: %w", err)
			}
		} else {
			return nil, err
		}
	}

	// Update the password for the admin user
	err = s.UpdatePassword(adminUser.ID, password)
	if err != nil {
		return nil, fmt.Errorf("failed to update admin password: %w", err)
	}

	// Return the admin user
	// Note: I need to return the user with the new password, because the user lost his password
	adminUser.Password = password // Set the password to the generated one

	return adminUser, nil
}

func (s *UserService) Close() error {
	return s.store.Close()
}

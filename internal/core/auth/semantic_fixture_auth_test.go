package auth

import (
	"encoding/json"
	"errors"

	"github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/perber/wiki/internal/core/tree"
)

var (
	errAuthSessionUnexpectedlyInactive  = errors.New("auth session unexpectedly inactive")
	errAuthSessionUnexpectedlyActive    = errors.New("auth session unexpectedly active")
	errAuthSessionCheckSucceeded        = errors.New("auth session check unexpectedly succeeded")
	errAuthPasswordUnexpectedlyRejected = errors.New("auth password unexpectedly rejected")
	errAuthPasswordUnexpectedlyAccepted = errors.New("auth password unexpectedly accepted")
	errAuthPasswordRejectionMissing     = errors.New("auth password rejection missing")
)

func matchAuthJSONSyntaxError() types.GomegaMatcher {
	return gomega.WithTransform(func(err error) *json.SyntaxError {
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			return syntaxErr
		}
		return nil
	}, gomega.Not(gomega.BeNil()))
}

func newFixtureSessionID[T ~string](raw T) SessionID {
	return NewSessionIDUnchecked(string(raw))
}

func newFixtureAPIKeyID[T ~string](raw T) APIKeyID {
	return NewAPIKeyIDUnchecked(string(raw))
}

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return NewUserIDUnchecked(string(raw))
}

func activeAuthSession(active bool, err error) error {
	if err != nil {
		return err
	}
	if !active {
		return errAuthSessionUnexpectedlyInactive
	}
	return nil
}

func inactiveAuthSession(active bool, err error) error {
	if err != nil {
		return err
	}
	if active {
		return errAuthSessionUnexpectedlyActive
	}
	return nil
}

func failedAuthSessionCheck(active bool, err error) error {
	if err != nil {
		return err
	}
	if active {
		return errAuthSessionUnexpectedlyActive
	}
	return errAuthSessionCheckSucceeded
}

func acceptedAuthPassword(matched bool, err error) error {
	if err != nil {
		return err
	}
	if !matched {
		return errAuthPasswordUnexpectedlyRejected
	}
	return nil
}

func rejectedAuthPassword(matched bool, err error) error {
	if matched {
		return errAuthPasswordUnexpectedlyAccepted
	}
	if err == nil {
		return errAuthPasswordRejectionMissing
	}
	return err
}

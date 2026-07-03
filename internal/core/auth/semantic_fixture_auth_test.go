package auth

import (
	"encoding/json"
	"errors"

	"github.com/onsi/gomega/gcustom"
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
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		var syntaxErr *json.SyntaxError
		return errors.As(err, &syntaxErr), nil
	}).WithMessage("match auth JSON syntax error")
}

func matchAuthSQLRowScanFailure() types.GomegaMatcher {
	return gcustom.MakeMatcher(func(err error) (bool, error) {
		return err != nil && errors.Unwrap(err) != nil, nil
	}).WithMessage("match auth SQL row scan failure")
}

func newFixtureSessionID[T ~string](raw T) SessionID {
	return NewSessionIDUnchecked(string(raw))
}

func newFixtureAPIKeyID[T ~string](raw T) APIKeyID {
	return NewAPIKeyIDUnchecked(string(raw))
}

func newFixtureUserID[T ~string](raw T) tree.UserID {
	return tree.NewUserIDUnchecked(string(raw))
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

package projectdaemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"

	"github.com/onsi/gomega/types"

	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/workspaceid"
)

type projectdaemonErrorMatcher struct {
	label string
	match func(error) bool
}

func matchActorContextValidationRejection() types.GomegaMatcher {
	return projectdaemonErrorMatcher{
		label: "actor context validation rejection",
		match: func(err error) bool {
			return err != nil && !errors.Is(err, errDecodeActorContext) && !errors.Is(err, errDecodeActorContextJSON)
		},
	}
}

func matchProjectdaemonJSONSyntaxError() types.GomegaMatcher {
	return projectdaemonErrorMatcher{
		label: "project daemon JSON syntax error",
		match: func(err error) bool {
			var syntaxErr *json.SyntaxError
			return errors.As(err, &syntaxErr) || errors.Is(err, io.ErrUnexpectedEOF)
		},
	}
}

func matchProjectdaemonJSONMarshalError() types.GomegaMatcher {
	return projectdaemonErrorMatcher{
		label: "project daemon JSON marshal error",
		match: func(err error) bool {
			var unsupportedType *json.UnsupportedTypeError
			var unsupportedValue *json.UnsupportedValueError
			return errors.As(err, &unsupportedType) || errors.As(err, &unsupportedValue)
		},
	}
}

func matchProjectdaemonPathError() types.GomegaMatcher {
	return projectdaemonErrorMatcher{
		label: "project daemon path error",
		match: func(err error) bool {
			var pathErr *os.PathError
			return errors.As(err, &pathErr)
		},
	}
}

func matchProjectdaemonURLParseError() types.GomegaMatcher {
	return projectdaemonErrorMatcher{
		label: "project daemon URL parse error",
		match: func(err error) bool {
			var urlErr *url.Error
			return errors.As(err, &urlErr)
		},
	}
}

func matchWorkspaceIDValidationError(code sharederrors.ErrorCode) types.GomegaMatcher {
	return projectdaemonErrorMatcher{
		label: fmt.Sprintf("workspace ID validation error %s", code),
		match: func(err error) bool {
			var validationErr *workspaceid.ValidationError
			return errors.As(err, &validationErr) &&
				validationErr.Code == code &&
				validationErr.MessageID == sharederrors.MessageIDForCode(code)
		},
	}
}

func (matcher projectdaemonErrorMatcher) Match(actual interface{}) (bool, error) {
	err, ok := actual.(error)
	if !ok {
		return false, fmt.Errorf("expected error, got %T", actual)
	}
	return matcher.match(err), nil
}

func (matcher projectdaemonErrorMatcher) FailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nto satisfy %s", actual, matcher.label)
}

func (matcher projectdaemonErrorMatcher) NegatedFailureMessage(actual interface{}) string {
	return fmt.Sprintf("Expected\n\t%#v\nnot to satisfy %s", actual, matcher.label)
}

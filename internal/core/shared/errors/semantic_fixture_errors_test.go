package errors_test

import sharederrors "github.com/perber/wiki/internal/core/shared/errors"

func newFixtureErrorCode[T ~string](raw T) sharederrors.ErrorCode {
	return sharederrors.ErrorCode(raw)
}

func newFixtureFieldErrorCode[T ~string](raw T) sharederrors.FieldErrorCode {
	return sharederrors.FieldErrorCode(raw)
}

func newFixtureMessageID[T ~string](raw T) sharederrors.MessageID {
	return sharederrors.MessageID(raw)
}

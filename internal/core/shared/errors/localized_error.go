package errors

import (
	stderrors "errors"
	"fmt"
	"strings"

	"github.com/perber/wiki/internal/localization"
)

type ErrorCode string

func (c ErrorCode) String() string {
	return string(c)
}

type MessageID string

func (id MessageID) String() string {
	return string(id)
}

type ErrorDefinition struct {
	Code      ErrorCode
	MessageID MessageID
	Message   string
	Template  string
}

type LocalizedError struct {
	Code      ErrorCode
	MessageID MessageID
	Message   string
	Template  string
	Args      []string
	Cause     error
}

type LocalizedErrorDetail struct {
	Code      ErrorCode `json:"code"`
	MessageID MessageID `json:"messageId,omitempty"`
	Message   string    `json:"message"`
	Template  string    `json:"template"`
	Args      []string  `json:"args,omitempty"`
}

func (e *LocalizedError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *LocalizedError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func NewLocalizedError(code ErrorCode, message, template string, cause error, args ...string) *LocalizedError {
	return &LocalizedError{
		Code:      code,
		MessageID: MessageIDForCode(code),
		Message:   message,
		Template:  template,
		Args:      append([]string(nil), args...),
		Cause:     cause,
	}
}

func NewLocalizedErrorFromCode(code ErrorCode, cause error, args ...string) *LocalizedError {
	messageID := MessageIDForCode(code)
	message := renderMessage(messageID, "", args...)
	return &LocalizedError{
		Code:      code,
		MessageID: messageID,
		Message:   message,
		Template:  message,
		Args:      append([]string(nil), args...),
		Cause:     cause,
	}
}

func NewLocalizedErrorFromCodeWithFallback(code ErrorCode, message, template string, cause error, args ...string) *LocalizedError {
	messageID := MessageIDForCode(code)
	return &LocalizedError{
		Code:      code,
		MessageID: messageID,
		Message:   renderMessage(messageID, message, args...),
		Template:  template,
		Args:      append([]string(nil), args...),
		Cause:     cause,
	}
}

func NewDefinedLocalizedError(definition ErrorDefinition, cause error, args ...string) *LocalizedError {
	messageID := definition.MessageID
	if messageID == "" {
		messageID = MessageIDForCode(definition.Code)
	}
	return &LocalizedError{
		Code:      definition.Code,
		MessageID: messageID,
		Message:   definition.Message,
		Template:  definition.Template,
		Args:      append([]string(nil), args...),
		Cause:     cause,
	}
}

func NewLocalizedErrorDetail(code ErrorCode, message, template string, args ...string) LocalizedErrorDetail {
	messageID := MessageIDForCode(code)
	return LocalizedErrorDetail{
		Code:      code,
		MessageID: messageID,
		Message:   renderMessage(messageID, message, args...),
		Template:  template,
		Args:      append([]string(nil), args...),
	}
}

func LocalizedErrorDetailFromError(err *LocalizedError) LocalizedErrorDetail {
	if err == nil {
		return LocalizedErrorDetail{}
	}
	messageID := err.MessageID
	if messageID == "" {
		messageID = MessageIDForCode(err.Code)
	}
	return LocalizedErrorDetail{
		Code:      err.Code,
		MessageID: messageID,
		Message:   renderMessage(messageID, err.Message, err.Args...),
		Template:  err.Template,
		Args:      append([]string(nil), err.Args...),
	}
}

func renderMessage(messageID MessageID, defaultEnglish string, args ...string) string {
	rendered := localization.English.Render(messageID, defaultEnglish, args...)
	return rendered.Message
}

func MessageIDForCode(code ErrorCode) MessageID {
	trimmed := strings.TrimSpace(string(code))
	if trimmed == "" {
		return ""
	}
	head, tail, ok := strings.Cut(trimmed, "_")
	if !ok || strings.TrimSpace(tail) == "" {
		return MessageID("errors." + trimmed)
	}
	return MessageID("errors." + head + "." + tail)
}

func AsLocalizedError(err error) (*LocalizedError, bool) {
	var localized *LocalizedError
	if !stderrors.As(err, &localized) {
		return nil, false
	}
	return localized, true
}

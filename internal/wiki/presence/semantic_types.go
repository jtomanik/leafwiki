package presence

import (
	"encoding/json"
	"strings"
)

type SessionType string
type SessionMode string
type SessionState string

const (
	SessionTypeWeb   SessionType = "web"
	SessionTypeAgent SessionType = "agent"
)

const (
	sessionModeViewRaw     = "view"
	sessionModeEditRaw     = "edit"
	sessionModeHistoryRaw  = "history"
	sessionModeAssetsRaw   = "assets"
	sessionModeSettingsRaw = "settings"
	sessionModeImportRaw   = "import"
	sessionModeUnknownRaw  = "unknown"
)

const (
	SessionModeView     SessionMode = sessionModeViewRaw
	SessionModeEdit     SessionMode = sessionModeEditRaw
	SessionModeHistory  SessionMode = sessionModeHistoryRaw
	SessionModeAssets   SessionMode = sessionModeAssetsRaw
	SessionModeSettings SessionMode = sessionModeSettingsRaw
	SessionModeImport   SessionMode = sessionModeImportRaw
	SessionModeUnknown  SessionMode = sessionModeUnknownRaw
	sessionModeInvalid  SessionMode = "__invalid__"
)

const (
	SessionStateActive SessionState = "active"
)

var validModes = map[SessionMode]struct{}{
	SessionModeView:     {},
	SessionModeEdit:     {},
	SessionModeHistory:  {},
	SessionModeAssets:   {},
	SessionModeSettings: {},
	SessionModeImport:   {},
	SessionModeUnknown:  {},
}

var sessionModesByRaw = map[string]SessionMode{
	sessionModeViewRaw:     SessionModeView,
	sessionModeEditRaw:     SessionModeEdit,
	sessionModeHistoryRaw:  SessionModeHistory,
	sessionModeAssetsRaw:   SessionModeAssets,
	sessionModeSettingsRaw: SessionModeSettings,
	sessionModeImportRaw:   SessionModeImport,
	sessionModeUnknownRaw:  SessionModeUnknown,
}

func SessionModeFromString(raw string) SessionMode {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if mode, ok := sessionModesByRaw[trimmed]; ok {
		return mode
	}
	return sessionModeInvalid
}

func (mode SessionMode) Normalize() SessionMode {
	switch mode {
	case "", SessionModeView, SessionModeEdit, SessionModeHistory, SessionModeAssets, SessionModeSettings, SessionModeImport, SessionModeUnknown:
		return mode
	default:
		return sessionModeInvalid
	}
}

func (mode SessionMode) IsValid() bool {
	_, ok := validModes[mode]
	return ok
}

type WebSessionID struct {
	value string
}

func (id WebSessionID) String() string {
	return id.value
}

func (id WebSessionID) Normalize() WebSessionID {
	return WebSessionIDFromString(id.value)
}

func (id WebSessionID) Length() int {
	return len(id.value)
}

func (id WebSessionID) IsZero() bool {
	return id.value == ""
}

func (id WebSessionID) Less(other WebSessionID) bool {
	return id.value < other.value
}

func (id WebSessionID) MarshalJSON() ([]byte, error) {
	return json.Marshal(id.value)
}

func (id *WebSessionID) UnmarshalJSON(raw []byte) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	*id = WebSessionIDFromString(value)
	return nil
}

func WebSessionIDFromString(raw string) WebSessionID {
	return WebSessionID{value: strings.TrimSpace(raw)}
}

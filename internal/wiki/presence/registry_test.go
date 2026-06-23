package presence

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	coreauth "github.com/perber/wiki/internal/core/auth"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
)

func TestWebPresenceRegistryListGatesEmailAndExpires(t *testing.T) {
	now := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	registry := NewWebPresenceRegistry(time.Minute, func() time.Time { return now })

	if err := registry.Record(Heartbeat{
		SessionID: "tab-1",
		Mode:      "edit",
		Dirty:     true,
	}, &coreauth.User{
		ID:       "editor-1",
		Username: "Editor One",
		Email:    "editor@example.test",
		Role:     coreauth.RoleEditor,
	}, &PageRef{
		ID:    "page-1",
		Path:  "/docs/api",
		Title: "API",
	}); err != nil {
		t.Fatalf("Record returned error: %v", err)
	}

	editorView := registry.List(&coreauth.User{Role: coreauth.RoleEditor})
	if len(editorView) != 1 {
		t.Fatalf("editor sessions = %#v, want one session", editorView)
	}
	if editorView[0].User.Email != "" {
		t.Fatalf("editor-visible user = %#v, did not expect email", editorView[0].User)
	}
	if editorView[0].State != "active" || editorView[0].Dirty != true || editorView[0].Page == nil || editorView[0].Page.Title != "API" {
		t.Fatalf("editor-visible session = %#v, want active dirty page session", editorView[0])
	}
	if editorView[0].Type != SessionTypeWeb || editorView[0].Mode != SessionModeEdit || editorView[0].State != SessionStateActive {
		t.Fatalf("typed session fields = %#v", editorView[0])
	}
	raw, err := json.Marshal(editorView[0])
	if err != nil {
		t.Fatalf("marshal web session: %v", err)
	}
	if !strings.Contains(string(raw), `"type":"web"`) ||
		!strings.Contains(string(raw), `"mode":"edit"`) ||
		!strings.Contains(string(raw), `"state":"active"`) {
		t.Fatalf("web session JSON = %s, want string compatibility fields", raw)
	}

	adminView := registry.List(&coreauth.User{Role: coreauth.RoleAdmin})
	if len(adminView) != 1 || adminView[0].User.Email != "editor@example.test" {
		t.Fatalf("admin sessions = %#v, want email visible", adminView)
	}

	now = now.Add(61 * time.Second)
	expiredView := registry.List(&coreauth.User{Role: coreauth.RoleAdmin})
	if len(expiredView) != 0 {
		t.Fatalf("expired sessions = %#v, want none", expiredView)
	}
}

func TestWebPresenceRegistryRejectsInvalidHeartbeatWithoutDroppingExistingSession(t *testing.T) {
	registry := NewWebPresenceRegistry(time.Minute, nil)
	user := &coreauth.User{ID: "editor-1", Username: "Editor One", Role: coreauth.RoleEditor}
	if err := registry.Record(Heartbeat{SessionID: "tab-1", Mode: "view"}, user, nil); err != nil {
		t.Fatalf("Record valid heartbeat returned error: %v", err)
	}

	if err := registry.Record(Heartbeat{SessionID: "", Mode: "view"}, user, nil); err == nil {
		t.Fatalf("Record empty sessionId returned nil error")
	}
	if err := registry.Record(Heartbeat{SessionID: "tab-2", Mode: "invalid"}, user, nil); err == nil {
		t.Fatalf("Record invalid mode returned nil error")
	}
	if sessions := registry.List(&coreauth.User{Role: coreauth.RoleAdmin}); len(sessions) != 1 || sessions[0].SessionID != "tab-1" {
		t.Fatalf("sessions after invalid heartbeats = %#v, want original tab only", sessions)
	}
}

func TestWebPresenceRegistryInvalidHeartbeatReturnsStableCodes(t *testing.T) {
	registry := NewWebPresenceRegistry(time.Minute, nil)
	user := &coreauth.User{ID: "editor-1", Username: "Editor One", Role: coreauth.RoleEditor}

	tests := []struct {
		name      string
		heartbeat Heartbeat
		code      sharederrors.ErrorCode
		messageID sharederrors.MessageID
	}{
		{
			name:      "missing session",
			heartbeat: Heartbeat{SessionID: "", Mode: "view"},
			code:      "presence_session_id_required",
			messageID: "errors.presence.session_id_required",
		},
		{
			name:      "invalid mode",
			heartbeat: Heartbeat{SessionID: "tab-1", Mode: "invalid"},
			code:      "presence_mode_invalid",
			messageID: "errors.presence.mode_invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := registry.Record(tc.heartbeat, user, nil)
			localized, ok := sharederrors.AsLocalizedError(err)
			if !ok {
				t.Fatalf("Record error = %T %v, want LocalizedError", err, err)
			}
			if localized.Code != tc.code {
				t.Fatalf("code = %q, want %q", localized.Code, tc.code)
			}
			if localized.MessageID != tc.messageID {
				t.Fatalf("messageId = %q, want %q", localized.MessageID, tc.messageID)
			}
		})
	}
}

func TestPresenceHeartbeatRouteReturnsStableErrorDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := NewWebPresenceRegistry(time.Minute, nil)
	routes := NewRoutes(RoutesConfig{Registry: registry})
	router := gin.New()
	router.POST("/heartbeat", func(c *gin.Context) {
		c.Set("user", &coreauth.User{ID: "editor-1", Username: "Editor One", Role: coreauth.RoleEditor})
		routes.handleHeartbeat(c)
	})

	req := httptest.NewRequest(http.MethodPost, "/heartbeat", strings.NewReader(`{"sessionId":"","mode":"view"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
	var body struct {
		Error sharederrors.LocalizedErrorDetail `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	if body.Error.Code != ErrCodePresenceSessionIDRequired {
		t.Fatalf("error.code = %q, want %q; body=%s", body.Error.Code, ErrCodePresenceSessionIDRequired, rec.Body.String())
	}
	if body.Error.MessageID != "errors.presence.session_id_required" {
		t.Fatalf("error.messageId = %q, want errors.presence.session_id_required", body.Error.MessageID)
	}
}

func TestWebPresenceRegistryBindsSessionIDToUser(t *testing.T) {
	registry := NewWebPresenceRegistry(time.Minute, nil)
	editor := &coreauth.User{ID: "editor-1", Username: "Editor One", Role: coreauth.RoleEditor}
	other := &coreauth.User{ID: "editor-2", Username: "Editor Two", Role: coreauth.RoleEditor}
	if err := registry.Record(Heartbeat{SessionID: "shared-tab", Mode: "view"}, editor, nil); err != nil {
		t.Fatalf("Record initial heartbeat returned error: %v", err)
	}

	if err := registry.Record(Heartbeat{SessionID: "shared-tab", Mode: "edit"}, other, nil); err == nil {
		t.Fatalf("Record cross-user heartbeat returned nil error")
	}
	if removed := registry.Remove("shared-tab", other); removed {
		t.Fatalf("Remove by different user returned true")
	}

	sessions := registry.List(&coreauth.User{Role: coreauth.RoleAdmin})
	if len(sessions) != 1 || sessions[0].Mode != "view" {
		t.Fatalf("sessions after cross-user attempts = %#v, want original view session", sessions)
	}
	if removed := registry.Remove("shared-tab", editor); !removed {
		t.Fatalf("Remove by owner returned false")
	}
	if sessions := registry.List(&coreauth.User{Role: coreauth.RoleAdmin}); len(sessions) != 0 {
		t.Fatalf("sessions after owner remove = %#v, want none", sessions)
	}
}

package auth

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var _ = ginkgo.Describe("session store", func() {
	ginkgo.It("TestSessionStore_CreateAndValidateSession", func() {
		t := ginkgo.GinkgoT()
		store, err := NewSessionStore(t.TempDir())
		if err != nil {
			t.Fatalf("NewSessionStore err: %v", err)
		}
		ginkgo.DeferCleanup(closeWithErrorCheck, store.Close)

		expiresAt := time.Now().Add(time.Hour)
		userID := newFixtureUserID("u1")
		sessionID := newFixtureSessionID("s1")
		if err := store.CreateSession(sessionID, userID, "refresh", expiresAt); err != nil {
			t.Fatalf("CreateSession err: %v", err)
		}

		active, err := store.IsActive(sessionID, userID, "refresh", time.Now())
		if err != nil {
			t.Fatalf("IsActive err: %v", err)
		}
		if !active {
			t.Fatalf("expected session to be active")
		}
	})

	ginkgo.It("TestSessionDatabasePath_WindowsPath", func() {
		t := ginkgo.GinkgoT()
		got := strings.ReplaceAll(sessionDatabasePath(`C:\wiki\data`, "sessions.db"), `\`, `/`)
		want := `C:/wiki/data/sessions.db`
		if got != want {
			t.Fatalf("path = %q, want %q", got, want)
		}
	})

	ginkgo.It("TestSessionStore_CreatesDatabaseInStorageDir", func() {
		t := ginkgo.GinkgoT()
		tmp := t.TempDir()
		store, err := NewSessionStore(tmp)
		if err != nil {
			t.Fatalf("NewSessionStore err: %v", err)
		}
		ginkgo.DeferCleanup(closeWithErrorCheck, store.Close)

		if _, err := os.Stat(filepath.Join(tmp, "sessions.db")); err != nil {
			t.Fatalf("expected sessions.db in storage dir, got err: %v", err)
		}
	})
})

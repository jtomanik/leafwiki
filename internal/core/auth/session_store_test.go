package auth

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var _ = ginkgo.Describe("session store", func() {
	ginkgo.It("stores refresh sessions and recognizes active credentials", func() {
		store, err := NewSessionStore(authTempDir())
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(closeWithErrorCheck, store.Close)

		expiresAt := time.Now().Add(time.Hour)
		userID := newFixtureUserID("u1")
		sessionID := newFixtureSessionID("s1")
		Expect(store.CreateSession(sessionID, userID, "refresh", expiresAt)).To(Succeed())

		active, err := store.IsActive(sessionID, userID, "refresh", time.Now())
		Expect(err).NotTo(HaveOccurred())
		Expect(active).To(BeTrue())
	})

	ginkgo.It("keeps Windows-style storage paths under the session database file", func() {
		got := strings.ReplaceAll(sessionDatabasePath(`C:\wiki\data`, "sessions.db"), `\`, `/`)
		want := `C:/wiki/data/sessions.db`
		Expect(got).To(Equal(want))
	})

	ginkgo.It("creates the session database inside the configured storage directory", func() {
		tmp := authTempDir()
		store, err := NewSessionStore(tmp)
		Expect(err).NotTo(HaveOccurred())
		ginkgo.DeferCleanup(closeWithErrorCheck, store.Close)

		_, err = os.Stat(filepath.Join(tmp, "sessions.db"))
		Expect(err).NotTo(HaveOccurred())
	})
})

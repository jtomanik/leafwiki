package projectdaemon

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/agenthooks"
)

var _ = ginkgo.Describe("project daemon deterministic edges", func() {
	ginkgo.It("uses registry fallback timestamps and prunes from expiry-loop ticks", ginkgo.Label("unit"), func() {
		now := time.Date(2026, 6, 27, 9, 0, 0, 0, time.UTC)
		presenceChanges := make(chan int, 2)
		presence := NewAgentPresenceRegistry(time.Millisecond, func(count int) {
			presenceChanges <- count
		})
		presence.now = func() time.Time {
			return now
		}
		event, err := normalizedAgentHookEventResult(agenthooks.ProviderCodex, []byte(`{"hook_event_name":"SessionStart","session_id":"codex-session"}`), time.Time{})
		Expect(err).To(Succeed())
		Expect(event).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Provider":      Equal(agenthooks.ProviderCodex),
			"EventName":     Equal(agenthooks.AgentEventSessionStart),
			"SessionIDHash": Not(BeEmpty()),
		}))

		presence.Record(event)
		Expect(presence.List()).To(HaveExactElements(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"FirstSeenAt": BeTemporally("==", now),
		})))
		Eventually(presenceChanges).Should(Receive(Equal(1)))

		now = now.Add(time.Hour)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			presence.RunExpiryLoop(ctx, time.Millisecond)
		}()
		Eventually(presenceChanges, 250*time.Millisecond).Should(Receive(BeZero()))
		cancel()
		Eventually(done, 250*time.Millisecond).Should(BeClosed())

		sessionChanges := make(chan int, 2)
		sessions := NewSessionRegistry(time.Millisecond, func(count int) {
			sessionChanges <- count
		})
		sessions.now = func() time.Time {
			return now
		}
		sessions.handles["session"] = now.Add(-time.Second)
		ctx, cancel = context.WithCancel(context.Background())
		done = make(chan struct{})
		go func() {
			defer close(done)
			sessions.RunExpiryLoop(ctx, time.Millisecond)
		}()
		Eventually(sessionChanges, 250*time.Millisecond).Should(Receive(BeZero()))
		cancel()
		Eventually(done, 250*time.Millisecond).Should(BeClosed())

		zeroTTL := NewSessionRegistry(time.Second, nil)
		zeroTTL.ttl = 0
		ctx, cancel = context.WithCancel(context.Background())
		cancel()
		Expect(func() { zeroTTL.RunExpiryLoop(ctx, 0) }).ToNot(Panic())
	})

	ginkgo.It("reports control client and server error boundaries", ginkgo.Label("integration"), func() {
		originalReadRandom := readRandom
		readRandom = func([]byte) (int, error) {
			return 0, errors.New("random failed")
		}
		server := NewControlServer(ControlServerOptions{
			Token:    "control-token",
			Sessions: NewSessionRegistry(time.Minute, nil),
			Health:   DaemonHealth{SchemaVersion: 1},
		})
		req := httptest.NewRequest(http.MethodPost, "/sessions", nil)
		req.Header.Set(ControlTokenHeader, "control-token")
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))
		readRandom = originalReadRandom

		req = httptest.NewRequest(http.MethodPost, "/agent-presence/events", strings.NewReader("{"))
		req.Header.Set(ControlTokenHeader, "control-token")
		rec = httptest.NewRecorder()
		NewControlServer(ControlServerOptions{
			Token:         "control-token",
			Sessions:      NewSessionRegistry(time.Minute, nil),
			AgentPresence: NewAgentPresenceRegistry(time.Minute, nil),
		}).ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusBadRequest))

		req = httptest.NewRequest(http.MethodGet, "/missing", nil)
		req.Header.Set(ControlTokenHeader, "control-token")
		rec = httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound))

		rec = httptest.NewRecorder()
		writeJSON(rec, func() {})
		Expect(rec).To(HaveHTTPStatus(http.StatusInternalServerError))

		marshalClient := NewClient("http://127.0.0.1", "control-token")
		Expect(marshalClient.doJSON(context.Background(), http.MethodPost, "/bad", func() {}, nil)).To(matchProjectdaemonJSONMarshalError())

		errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}))
		ginkgo.DeferCleanup(errorServer.Close)
		err := NewClient(errorServer.URL, "control-token").doJSON(context.Background(), http.MethodGet, "/empty-error", nil, nil)
		Expect(err).To(haveControlStatus(http.StatusServiceUnavailable))

		emptySessionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			_ = json.NewEncoder(w).Encode(SessionHandle{})
		}))
		ginkgo.DeferCleanup(emptySessionServer.Close)
		_, err = NewClient(emptySessionServer.URL, "control-token").RegisterSession(context.Background())
		Expect(err).To(MatchError(errDaemonEmptySessionID))

		badStatusServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			http.Error(w, "failed", http.StatusInternalServerError)
		}))
		ginkgo.DeferCleanup(badStatusServer.Close)
		_, err = NewClient(badStatusServer.URL, "control-token").RegisterSession(context.Background())
		Expect(err).To(haveControlStatus(http.StatusInternalServerError))
		_, err = NewClient(badStatusServer.URL, "control-token").ListAgentPresence(context.Background())
		Expect(err).To(haveControlStatus(http.StatusInternalServerError))

		var seenControlToken string
		authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seenControlToken = req.Header.Get(ControlTokenHeader)
			w.WriteHeader(http.StatusNoContent)
		}))
		ginkgo.DeferCleanup(authServer.Close)
		authClient := &http.Client{Transport: AuthRoundTripper{ControlToken: "control-token"}}
		resp, err := authClient.Get(authServer.URL)
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.Body.Close()).To(Succeed())
		Expect(seenControlToken).To(Equal("control-token"))
	})
})

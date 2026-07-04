package projectdaemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/agenthooks"
	sharederrors "github.com/perber/wiki/internal/core/shared/errors"
	"github.com/perber/wiki/internal/workspaceid"
)

type parsedControlErrorBody struct {
	Code      sharederrors.ErrorCode
	MessageID sharederrors.MessageID
}

func parsedControlErrorBodyForSpec(raw []byte) parsedControlErrorBody {
	code, messageID, _ := parseControlErrorBody(raw)
	return parsedControlErrorBody{
		Code:      code,
		MessageID: messageID,
	}
}

type fakeDescriptorTempFile struct {
	name      string
	chmodErr  error
	writeErrs map[int]error
	closeErr  error
	writes    int
}

func (f *fakeDescriptorTempFile) Name() string {
	return f.name
}

func (f *fakeDescriptorTempFile) Chmod(os.FileMode) error {
	return f.chmodErr
}

func (f *fakeDescriptorTempFile) Write(raw []byte) (int, error) {
	f.writes++
	if err := f.writeErrs[f.writes]; err != nil {
		return 0, err
	}
	return len(raw), nil
}

func (f *fakeDescriptorTempFile) Close() error {
	return f.closeErr
}

var _ = ginkgo.Describe("project daemon deterministic edges", func() {
	ginkgo.It("canonicalizes missing project paths and redacts config mismatch secrets", ginkgo.Label("integration"), func() {
		baseDir := tempProjectdaemonDir()
		dataDir := filepath.Join(baseDir, "missing", "data")
		rootDir := filepath.Join(baseDir, "missing", "root")
		resolvedBase, err := filepath.EvalSymlinks(baseDir)
		Expect(err).NotTo(HaveOccurred())

		canonicalData, canonicalRoot, err := CanonicalizeProject(dataDir, rootDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(canonicalData).To(Equal(filepath.Join(resolvedBase, "missing", "data")))
		Expect(canonicalRoot).To(Equal(filepath.Join(resolvedBase, "missing", "root")))

		token, err := RandomToken()
		Expect(err).NotTo(HaveOccurred())
		Expect(token).To(MatchRegexp(`^[0-9a-f]{64}$`))
		Expect(HashSecret(" \t\n ")).To(BeEmpty())
		Expect(HashSecret(" secret ")).To(Equal(HashSecret("secret")))

		owner := Config{PrivateMCPToken: "", CustomStylesheet: ""}
		requested := Config{PrivateMCPToken: "super-secret", CustomStylesheet: "custom.css"}
		mismatches := CompareConfig(owner, requested)
		formatted := FormatConfigMismatch(mismatches)
		Expect(formatted).To(ContainSubstring("private-mcp-token owner=empty requested=redacted"))
		Expect(formatted).To(ContainSubstring(`custom-stylesheet owner="" requested=custom.css`))
		Expect(formatted).NotTo(ContainSubstring("super-secret"))
		Expect(FormatConfigMismatch(nil)).To(Equal("project daemon config mismatch"))
		Expect(FormatConfigMismatch([]Mismatch{{Field: "flag-only"}})).To(Equal("project daemon config mismatch: flag-only"))
	})

	ginkgo.It("handles descriptor IO trust and write error edges", ginkgo.Label("integration"), func() {
		dataDir := tempProjectdaemonDir()
		path := DescriptorPath(dataDir)

		Expect(WriteDescriptorAtomic(path, nil)).To(MatchError(errDescriptorRequired))
		Expect(os.MkdirAll(filepath.Dir(path), 0o755)).To(Succeed())
		Expect(os.WriteFile(path, []byte("{"), 0o600)).To(Succeed())
		_, err := ReadDescriptor(path)
		Expect(err).To(matchProjectdaemonJSONSyntaxError())
		_, err = ReadDescriptor(filepath.Join(tempProjectdaemonDir(), "missing.json"))
		Expect(err).To(MatchError(os.ErrNotExist))
		_, err = ReadTrustedDescriptor(filepath.Join(tempProjectdaemonDir(), "missing.json"))
		Expect(err).To(MatchError(os.ErrNotExist))

		_, err = ReadTrustedDescriptor(filepath.Dir(path))
		Expect(err).To(MatchError(errDescriptorNotRegularFile))

		parentFile := filepath.Join(tempProjectdaemonDir(), "descriptor-parent-file")
		Expect(os.WriteFile(parentFile, []byte("not a directory"), 0o600)).To(Succeed())
		originalMkdirAllDescriptorPath := mkdirAllDescriptorPath
		mkdirDescriptorErr := errors.New("descriptor mkdir failed")
		mkdirAllDescriptorPath = func(string, os.FileMode) error {
			return mkdirDescriptorErr
		}
		ginkgo.DeferCleanup(func() {
			mkdirAllDescriptorPath = originalMkdirAllDescriptorPath
		})
		err = WriteDescriptorAtomic(filepath.Join(parentFile, "descriptor.json"), &Descriptor{SchemaVersion: DescriptorSchemaVersion})
		Expect(err).To(MatchError(mkdirDescriptorErr))
		mkdirAllDescriptorPath = originalMkdirAllDescriptorPath

		err = RemoveDescriptor(filepath.Join(parentFile, "descriptor.json"))
		Expect(err).To(matchProjectdaemonPathError())
	})

	ginkgo.It("rejects malformed actor context envelopes before trusting workspace identity", ginkgo.Label("unit"), func() {
		now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
		valid := ActorContext{
			Version:     1,
			Issuer:      ActorContextIssuerWikid,
			Subject:     "user:admin",
			WorkspaceID: "home",
			ExpiresAt:   now.Add(time.Minute),
		}
		Expect(validateActorContext(valid, ActorContextValidation{Now: now})).To(Succeed())

		_, err := DecodeActorContext(" ", ActorContextValidation{Now: now})
		Expect(err).To(MatchError(errActorContextRequired))
		_, err = DecodeActorContext("%%%invalid-base64", ActorContextValidation{Now: now})
		Expect(err).To(MatchError(errDecodeActorContext))
		encodedJSON := base64.RawURLEncoding.EncodeToString([]byte("{"))
		_, err = DecodeActorContext(encodedJSON, ActorContextValidation{Now: now})
		Expect(err).To(MatchError(errDecodeActorContextJSON))

		wire := actorContextWire{
			Version:     1,
			Issuer:      ActorContextIssuerWikid,
			Subject:     "user:admin",
			WorkspaceID: "not valid",
			ExpiresAt:   now.Add(time.Minute),
		}
		_, err = actorContextFromWire(wire)
		Expect(err).To(matchWorkspaceIDValidationError(workspaceid.ErrCodeWorkspaceIDInvalid))

		valid.Version = 2
		Expect(validateActorContext(valid, ActorContextValidation{Now: now})).To(MatchError(errActorContextVersion))
		valid.Version = 1
		valid.WorkspaceID = ""
		Expect(validateActorContext(valid, ActorContextValidation{Now: now})).To(MatchError(errActorContextWorkspaceRequired))
		valid.WorkspaceID = "home"
		valid.ExpiresAt = time.Time{}
		Expect(validateActorContext(valid, ActorContextValidation{Now: now})).To(MatchError(errActorContextExpired))
	})

	ginkgo.It("sorts agent presence, sanitizes metadata, and exits expiry loops on cancellation", ginkgo.Label("unit"), func() {
		registry := NewAgentPresenceRegistry(-1, nil)
		Expect(registry.ttl).To(Equal(DefaultIdleTimeout))
		registry.sessions[presenceKey(agenthooks.ProviderCursor, "b")] = AgentPresenceSession{Provider: agenthooks.ProviderCursor, SessionIDHash: "b"}
		registry.sessions[presenceKey(agenthooks.ProviderCodex, "z")] = AgentPresenceSession{Provider: agenthooks.ProviderCodex, SessionIDHash: "z"}
		registry.sessions[presenceKey(agenthooks.ProviderCodex, "a")] = AgentPresenceSession{Provider: agenthooks.ProviderCodex, SessionIDHash: "a"}

		sessions := registry.List()
		Expect(sessions).To(HaveExactElements(
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Provider":      Equal(agenthooks.ProviderCodex),
				"SessionIDHash": Equal("a"),
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Provider":      Equal(agenthooks.ProviderCodex),
				"SessionIDHash": Equal("z"),
			}),
			gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Provider":      Equal(agenthooks.ProviderCursor),
				"SessionIDHash": Equal("b"),
			}),
		))

		Expect(safeAgentSource("")).To(BeEmpty())
		Expect(safeAgentSource("Startup")).To(Equal(agenthooks.AgentSourceStartup))
		Expect(safeAgentSource("Hook")).To(Equal(agenthooks.AgentSourceHook))
		Expect(safeAgentSource("MCP")).To(Equal(agenthooks.AgentSourceMCP))
		Expect(safeAgentSource("Tool")).To(Equal(agenthooks.AgentSourceTool))
		Expect(safeAgentSource("User")).To(Equal(agenthooks.AgentSourceUser))
		Expect(safeAgentSource("IDE")).To(Equal(agenthooks.AgentSourceIDE))
		Expect(safeAgentSource("Agent")).To(Equal(agenthooks.AgentSourceAgent))
		Expect(safeAgentSource("unknown-client")).To(Equal(agenthooks.AgentSourceUnknown))
		Expect(safeAgentSource("bearer token")).To(BeEmpty())
		Expect(safeAgentMetadata("  one \t two  ", 80)).To(Equal("one two"))
		Expect(safeAgentMetadata("abcdef", 3)).To(Equal("abc"))
		Expect(safeAgentToolName(" secret-tool ")).To(BeEmpty())

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		done := make(chan struct{})
		go func() {
			defer close(done)
			registry.RunExpiryLoop(ctx, time.Hour)
		}()
		Eventually(done).
			WithTimeout(250*time.Millisecond).
			Should(BeClosed(), "AgentPresenceRegistry RunExpiryLoop should exit after context cancellation")

		zeroTTLRegistry := NewAgentPresenceRegistry(0, nil)
		ctx, cancel = context.WithCancel(context.Background())
		cancel()
		done = make(chan struct{})
		go func() {
			defer close(done)
			zeroTTLRegistry.RunExpiryLoop(ctx, 0)
		}()
		Eventually(done).
			WithTimeout(250*time.Millisecond).
			Should(BeClosed(), "AgentPresenceRegistry RunExpiryLoop should exit after interval defaulting")
	})

	ginkgo.It("uses default session registry TTLs and exits session expiry loops on cancellation", ginkgo.Label("unit"), func() {
		registry := NewSessionRegistry(0, nil)
		Expect(registry.ttl).To(Equal(DefaultHeartbeatTTL))
		Expect(registry).To(reportSeenSessionState(false, 0))

		random, err := randomID()
		Expect(err).NotTo(HaveOccurred())
		Expect(random).To(MatchRegexp(`^[0-9a-f]{32}$`))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		done := make(chan struct{})
		go func() {
			defer close(done)
			registry.RunExpiryLoop(ctx, 0)
		}()
		Eventually(done).
			WithTimeout(250*time.Millisecond).
			Should(BeClosed(), "SessionRegistry RunExpiryLoop should exit after context cancellation")
	})

	ginkgo.It("handles control client and control error parsing edge cases", ginkgo.Label("integration"), func() {
		Expect(&ControlHTTPError{StatusCode: http.StatusTeapot}).To(haveControlStatus(http.StatusTeapot))
		Expect(parsedControlErrorBodyForSpec([]byte(`{"error":{"code":"daemon_control_unauthorized","messageId":"errors.daemon.control_unauthorized","message":" unauthorized "}}`))).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Code":      Equal(errCodeDaemonControlUnauthorized),
			"MessageID": Equal(sharederrors.MessageIDForCode(errCodeDaemonControlUnauthorized)),
		}))
		Expect(parsedControlErrorBodyForSpec([]byte(" plain text "))).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Code":      BeEmpty(),
			"MessageID": BeEmpty(),
		}))

		var seenControlToken string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			seenControlToken = req.Header.Get(ControlTokenHeader)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(DaemonHealth{OK: false})
		}))
		ginkgo.DeferCleanup(server.Close)

		client := NewClient(server.URL, "control-token")
		_, err := client.Health(context.Background())
		Expect(err).To(MatchError(errDaemonHealthCheckFailed))
		Expect(seenControlToken).To(Equal("control-token"))

		badURLClient := NewClient("http://127.0.0.1:1/%zz", "control-token")
		err = badURLClient.Ping(context.Background())
		Expect(err).To(matchProjectdaemonURLParseError())

		failingClient := NewClient("http://127.0.0.1", "control-token")
		transportErr := errors.New("projectdaemon transport failed")
		failingClient.httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, transportErr
		})}
		err = failingClient.ReleaseSession(context.Background(), "session")
		Expect(err).To(MatchError(transportErr))

		noBodyClient := NewClient(server.URL, "control-token")
		err = noBodyClient.doJSON(context.Background(), http.MethodGet, "/health", nil, nil)
		Expect(err).To(Succeed())

		var _ workspaceid.WorkspaceID = "home"
	})

	ginkgo.It("surfaces actor-context and config failure paths", ginkgo.Label("unit"), func() {
		marshalActorContextErr := errors.New("actor context marshal failed")
		originalMarshalActorContext := marshalActorContextJSON
		marshalActorContextJSON = func(any) ([]byte, error) {
			return nil, marshalActorContextErr
		}
		_, err := EncodeActorContext(ActorContext{})
		Expect(err).To(MatchError(marshalActorContextErr))
		marshalActorContextJSON = originalMarshalActorContext

		wire := actorContextWire{
			Version:     1,
			Issuer:      ActorContextIssuerWikid,
			Subject:     "user:admin",
			WorkspaceID: "not valid",
			ExpiresAt:   time.Now().Add(time.Minute),
		}
		raw, err := json.Marshal(wire)
		Expect(err).ToNot(HaveOccurred())
		_, err = DecodeActorContext(base64.RawURLEncoding.EncodeToString(raw), ActorContextValidation{})
		Expect(err).To(matchWorkspaceIDValidationError(workspaceid.ErrCodeWorkspaceIDInvalid))

		Expect(validateActorContext(ActorContext{
			Version:     1,
			Issuer:      ActorContextIssuerWikid,
			Subject:     "user:admin",
			WorkspaceID: "home",
			ExpiresAt:   time.Now().Add(time.Minute),
		}, ActorContextValidation{})).To(Succeed())

		marshalConfigErr := errors.New("config marshal failed")
		originalMarshalConfig := marshalConfigJSON
		marshalConfigJSON = func(any) ([]byte, error) {
			return nil, marshalConfigErr
		}
		_, err = ConfigHash(Config{})
		Expect(err).To(MatchError(marshalConfigErr))
		marshalConfigJSON = originalMarshalConfig

		readRandomErr := errors.New("random failed")
		originalReadRandom := readRandom
		readRandom = func([]byte) (int, error) {
			return 0, readRandomErr
		}
		_, err = RandomToken()
		Expect(err).To(MatchError(readRandomErr))
		_, err = randomID()
		Expect(err).To(MatchError(readRandomErr))
		_, err = NewSessionRegistry(time.Minute, nil).Register()
		Expect(err).To(MatchError(readRandomErr))
		readRandom = originalReadRandom
	})

	ginkgo.It("reports canonical path resolution errors deterministically", ginkgo.Label("unit"), func() {
		originalAbsPath := absPath
		originalEvalSymlinks := evalSymlinks
		ginkgo.DeferCleanup(func() {
			absPath = originalAbsPath
			evalSymlinks = originalEvalSymlinks
		})

		absErr := errors.New("projectdaemon data path failed")
		absPath = func(string) (string, error) {
			return "", absErr
		}
		_, _, err := CanonicalizeProject("data", "root")
		Expect(err).To(MatchError(absErr))

		calls := 0
		rootAbsErr := errors.New("projectdaemon root path failed")
		absPath = func(path string) (string, error) {
			calls++
			if calls == 2 {
				return "", rootAbsErr
			}
			return filepath.Join(string(filepath.Separator), "data"), nil
		}
		evalSymlinks = func(path string) (string, error) {
			return path, nil
		}
		_, _, err = CanonicalizeProject("data", "root")
		Expect(err).To(MatchError(rootAbsErr))

		absPath = func(string) (string, error) {
			return filepath.Join(string(filepath.Separator), "denied"), nil
		}
		evalSymlinks = func(string) (string, error) {
			return "", os.ErrPermission
		}
		_, err = canonicalPath("denied")
		Expect(err).To(MatchError(os.ErrPermission))

		absPath = func(string) (string, error) {
			return filepath.Join(string(filepath.Separator), "missing", "child"), nil
		}
		evalSymlinks = func(path string) (string, error) {
			if filepath.Base(path) == "missing" {
				return "", os.ErrPermission
			}
			return "", os.ErrNotExist
		}
		_, err = canonicalPath("missing/child")
		Expect(err).To(MatchError(os.ErrPermission))

		absPath = func(string) (string, error) {
			return string(filepath.Separator), nil
		}
		evalSymlinks = func(string) (string, error) {
			return "", os.ErrNotExist
		}
		resolved, err := canonicalPath("missing-to-root")
		Expect(err).ToNot(HaveOccurred())
		Expect(resolved).To(Equal(filepath.Clean(string(filepath.Separator))))
	})

	ginkgo.It("reports descriptor atomic-write failure branches", ginkgo.Label("integration"), func() {
		tmp := tempProjectdaemonDir()
		desc := &Descriptor{SchemaVersion: DescriptorSchemaVersion}

		originalMarshalDescriptor := marshalDescriptorJSON
		originalCreateDescriptorTempFile := createDescriptorTempFile
		originalRenameTemporaryDescriptor := renameTemporaryDescriptor
		originalChmodDescriptorFile := chmodDescriptorFile
		ginkgo.DeferCleanup(func() {
			marshalDescriptorJSON = originalMarshalDescriptor
			createDescriptorTempFile = originalCreateDescriptorTempFile
			renameTemporaryDescriptor = originalRenameTemporaryDescriptor
			chmodDescriptorFile = originalChmodDescriptorFile
		})

		marshalErr := errors.New("descriptor marshal failed")
		marshalDescriptorJSON = func(any, string, string) ([]byte, error) {
			return nil, marshalErr
		}
		Expect(WriteDescriptorAtomic(filepath.Join(tmp, "marshal.json"), desc)).To(MatchError(marshalErr))
		marshalDescriptorJSON = originalMarshalDescriptor

		createErr := errors.New("create failed")
		chmodTempErr := errors.New("chmod failed")
		writeErr := errors.New("write failed")
		newlineErr := errors.New("newline failed")
		closeErr := errors.New("close failed")
		renameErr := errors.New("rename failed")
		finalChmodErr := errors.New("final chmod failed")
		cases := []struct {
			name      string
			temp      *fakeDescriptorTempFile
			createErr error
			renameErr error
			chmodErr  error
			wantErr   error
		}{
			{name: "create", createErr: createErr, wantErr: createErr},
			{name: "chmod temp", temp: &fakeDescriptorTempFile{name: filepath.Join(tmp, "chmod.tmp"), chmodErr: chmodTempErr}, wantErr: chmodTempErr},
			{name: "write body", temp: &fakeDescriptorTempFile{name: filepath.Join(tmp, "write.tmp"), writeErrs: map[int]error{1: writeErr}}, wantErr: writeErr},
			{name: "write newline", temp: &fakeDescriptorTempFile{name: filepath.Join(tmp, "newline.tmp"), writeErrs: map[int]error{2: newlineErr}}, wantErr: newlineErr},
			{name: "close", temp: &fakeDescriptorTempFile{name: filepath.Join(tmp, "close.tmp"), closeErr: closeErr}, wantErr: closeErr},
			{name: "rename", temp: &fakeDescriptorTempFile{name: filepath.Join(tmp, "rename.tmp")}, renameErr: renameErr, wantErr: renameErr},
			{name: "chmod final", temp: &fakeDescriptorTempFile{name: filepath.Join(tmp, "final.tmp")}, chmodErr: finalChmodErr, wantErr: finalChmodErr},
		}

		for _, tt := range cases {
			tt := tt
			createDescriptorTempFile = func(string, string) (descriptorTempFile, error) {
				if tt.createErr != nil {
					return nil, tt.createErr
				}
				return tt.temp, nil
			}
			renameTemporaryDescriptor = func(string, string) error {
				return tt.renameErr
			}
			chmodDescriptorFile = func(string, os.FileMode) error {
				return tt.chmodErr
			}

			err := WriteDescriptorAtomic(filepath.Join(tmp, tt.name+".json"), desc)
			Expect(err).To(MatchError(tt.wantErr))
		}
	})

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

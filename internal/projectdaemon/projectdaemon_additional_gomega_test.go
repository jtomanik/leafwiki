package projectdaemon

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/agenthooks"
	"github.com/perber/wiki/internal/workspaceid"
)

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
		Expect(registry).To(reportNoSessionSeen(0))

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
})

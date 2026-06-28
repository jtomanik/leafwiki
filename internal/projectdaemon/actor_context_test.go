package projectdaemon

import (
	ginkgo "github.com/onsi/ginkgo/v2"
	"strings"
	"time"

	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.It("TestActorContextRoundTripValidatesPrivateEnvelope", func() {
	t := ginkgo.GinkgoT()
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	ctx := ActorContext{
		Version:     1,
		Issuer:      ActorContextIssuerWikid,
		Subject:     "user:admin",
		Username:    "admin",
		Role:        "admin",
		Scopes:      []string{"leafwiki:workspace:read", "leafwiki:workspace:write", "leafwiki:mcp"},
		WorkspaceID: "current",
		AuthMethod:  "disabled",
		IssuedAt:    now,
		ExpiresAt:   now.Add(5 * time.Minute),
	}

	encoded, err := EncodeActorContext(ctx)
	if err != nil {
		t.Fatalf("EncodeActorContext failed: %v", err)
	}
	if encoded == "" || strings.Contains(encoded, "{") || strings.Contains(encoded, "+") || strings.Contains(encoded, "/") {
		t.Fatalf("encoded actor context = %q, want base64url payload", encoded)
	}

	decoded, err := DecodeActorContext(encoded, ActorContextValidation{
		Now:         now.Add(time.Minute),
		WorkspaceID: "current",
	})
	if err != nil {
		t.Fatalf("DecodeActorContext failed: %v", err)
	}
	if decoded.Subject != "user:admin" || decoded.Role != "admin" || decoded.AuthMethod != "disabled" {
		t.Fatalf("decoded actor context = %#v", decoded)
	}

})

var _ = ginkgo.It("TestActorContextValidationCarriesSemanticWorkspaceID", func() {
	validation := ActorContextValidation{WorkspaceID: workspaceid.WorkspaceID("current")}

	var _ workspaceid.WorkspaceID = validation.WorkspaceID

})

var _ = ginkgo.DescribeTable("ActorContext.SubjectID",
	func(subject string, want string) {
		t := ginkgo.GinkgoT()
		if got := (ActorContext{Subject: subject}).SubjectID(); got != want {
			t.Fatalf("SubjectID(%q) = %q, want %q", subject, got, want)
		}
	},
	ginkgo.Entry("trims user subject prefix", " user:admin ", "admin"),
	ginkgo.Entry("preserves non-user subject namespace", "service:wikid", "service:wikid"),
	ginkgo.Entry("trims bare subject", "  admin  ", "admin"),
)

var _ = ginkgo.DescribeTable("TestDecodeActorContextRejectsSpoofedOrStaleEnvelope",
	func(mutate func(*ActorContext)) {
		t := ginkgo.GinkgoT()
		now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
		ctx := ActorContext{
			Version:     1,
			Issuer:      ActorContextIssuerWikid,
			Subject:     "user:admin",
			Username:    "admin",
			Role:        "admin",
			WorkspaceID: "current",
			AuthMethod:  "cookie",
			IssuedAt:    now,
			ExpiresAt:   now.Add(5 * time.Minute),
		}
		mutate(&ctx)

		encoded, err := EncodeActorContext(ctx)
		if err != nil {
			t.Fatalf("EncodeActorContext failed: %v", err)
		}
		if _, err := DecodeActorContext(encoded, ActorContextValidation{Now: now, WorkspaceID: "current"}); err == nil {
			t.Fatalf("DecodeActorContext unexpectedly accepted context")
		}
	},
	ginkgo.Entry("expired", func(ctx *ActorContext) { ctx.ExpiresAt = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC).Add(-time.Second) }),
	ginkgo.Entry("wrong issuer", func(ctx *ActorContext) { ctx.Issuer = "frontd" }),
	ginkgo.Entry("wrong workspace", func(ctx *ActorContext) { ctx.WorkspaceID = "other" }),
	ginkgo.Entry("missing subject", func(ctx *ActorContext) { ctx.Subject = "" }),
)

var _ = ginkgo.It("TestDecodeActorContextRejectsSpoofedOrStaleEnvelope malformed payload", func() {
	t := ginkgo.GinkgoT()
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

	if _, err := DecodeActorContext("not json", ActorContextValidation{Now: now, WorkspaceID: "current"}); err == nil {
		t.Fatalf("DecodeActorContext accepted malformed payload")
	}
})

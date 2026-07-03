package projectdaemon

import (
	"time"

	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gstruct"

	"github.com/perber/wiki/internal/workspaceid"
)

var _ = ginkgo.Describe("actor context envelopes", func() {
	ginkgo.It("round-trips private actor context without exposing raw JSON bytes", func() {
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
		Expect(err).NotTo(HaveOccurred())
		Expect(encoded).To(SatisfyAll(Not(BeEmpty()), Not(ContainSubstring("{")), Not(ContainSubstring("+")), Not(ContainSubstring("/"))))

		decoded, err := DecodeActorContext(encoded, ActorContextValidation{
			Now:         now.Add(time.Minute),
			WorkspaceID: "current",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(decoded).To(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Subject":    Equal("user:admin"),
			"Role":       Equal("admin"),
			"AuthMethod": Equal("disabled"),
		}))
	})

	ginkgo.It("keeps validation workspace IDs semantically typed", func() {
		validation := ActorContextValidation{WorkspaceID: workspaceid.WorkspaceID("current")}

		var _ workspaceid.WorkspaceID = validation.WorkspaceID
	})

	ginkgo.DescribeTable("subject identifiers",
		func(subject string, want string) {
			Expect((ActorContext{Subject: subject}).SubjectID()).To(Equal(want))
		},
		ginkgo.Entry("trim user subject prefixes", " user:admin ", "admin"),
		ginkgo.Entry("preserve non-user subject namespaces", "service:wikid", "service:wikid"),
		ginkgo.Entry("trim bare subjects", "  admin  ", "admin"),
	)

	ginkgo.DescribeTable("rejects spoofed or stale private envelopes",
		func(mutate func(*ActorContext)) {
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
			Expect(err).NotTo(HaveOccurred())
			_, err = DecodeActorContext(encoded, ActorContextValidation{Now: now, WorkspaceID: "current"})
			Expect(err).To(HaveOccurred())
		},
		ginkgo.Entry("expired envelope", func(ctx *ActorContext) {
			ctx.ExpiresAt = time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC).Add(-time.Second)
		}),
		ginkgo.Entry("wrong issuer", func(ctx *ActorContext) { ctx.Issuer = "frontd" }),
		ginkgo.Entry("wrong workspace", func(ctx *ActorContext) { ctx.WorkspaceID = "other" }),
		ginkgo.Entry("missing subject", func(ctx *ActorContext) { ctx.Subject = "" }),
	)

	ginkgo.It("rejects malformed private envelopes before decoding actor metadata", func() {
		now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

		_, err := DecodeActorContext("not json", ActorContextValidation{Now: now, WorkspaceID: "current"})

		Expect(err).To(HaveOccurred())
	})
})

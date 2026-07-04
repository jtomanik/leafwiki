package oauth

import (
	"context"
	"fmt"
	ginkgo "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sync"

	"github.com/ory/fosite"
)

var _ = ginkgo.Describe("Fosite in-memory store", ginkgo.Label("integration"), func() {
	ginkgo.It("creates and retrieves public OAuth clients", func() {
		ctx := context.Background()
		store := newFositeStore()

		_, err := store.GetClient(ctx, ClientID)
		Expect(err).To(MatchError(fosite.ErrNotFound))

		Expect(store.setClient(fixedOAuthClient())).To(Succeed())
		client, err := store.GetClient(ctx, ClientID)
		Expect(err).NotTo(HaveOccurred())
		Expect(client).To(matchPublicOAuthClient(ClientID))
	})

	ginkgo.It("keeps invalidated authorization code requesters available for Fosite error handling", func() {
		ctx := context.Background()
		store := newFositeStore()
		requester := newStoreTestRequester("authorize-request")

		Expect(store.CreateAuthorizeCodeSession(ctx, "code-signature", requester)).To(Succeed())
		loaded, err := store.GetAuthorizeCodeSession(ctx, "code-signature", newFositeSession("", ""))
		Expect(err).NotTo(HaveOccurred())
		Expect(loaded.GetID()).To(Equal("authorize-request"))

		Expect(store.InvalidateAuthorizeCodeSession(ctx, "code-signature")).To(Succeed())
		invalidated, err := store.GetAuthorizeCodeSession(ctx, "code-signature", newFositeSession("", ""))
		Expect(err).To(MatchError(fosite.ErrInvalidatedAuthorizeCode))
		Expect(invalidated.GetID()).To(Equal("authorize-request"))
	})

	ginkgo.It("creates, retrieves, and deletes PKCE request sessions", func() {
		ctx := context.Background()
		store := newFositeStore()
		requester := newStoreTestRequester("pkce-request")

		Expect(store.CreatePKCERequestSession(ctx, "code-signature", requester)).To(Succeed())
		loaded, err := store.GetPKCERequestSession(ctx, "code-signature", newFositeSession("", ""))
		Expect(err).NotTo(HaveOccurred())
		Expect(loaded.GetID()).To(Equal("pkce-request"))
		Expect(store.DeletePKCERequestSession(ctx, "code-signature")).To(Succeed())
		_, err = store.GetPKCERequestSession(ctx, "code-signature", newFositeSession("", ""))
		Expect(err).To(MatchError(fosite.ErrNotFound))
	})

	ginkgo.It("creates, retrieves, and deletes access token sessions", func() {
		ctx := context.Background()
		store := newFositeStore()
		requester := newStoreTestRequester("access-request")

		Expect(store.CreateAccessTokenSession(ctx, "access-signature", requester)).To(Succeed())
		loaded, err := store.GetAccessTokenSession(ctx, "access-signature", newFositeSession("", ""))
		Expect(err).NotTo(HaveOccurred())
		Expect(loaded.GetID()).To(Equal("access-request"))
		Expect(store.DeleteAccessTokenSession(ctx, "access-signature")).To(Succeed())
		_, err = store.GetAccessTokenSession(ctx, "access-signature", newFositeSession("", ""))
		Expect(err).To(MatchError(fosite.ErrNotFound))
	})

	ginkgo.It("creates, deletes, and rotates refresh token sessions while revoking the linked access token", func() {
		ctx := context.Background()
		store := newFositeStore()
		requester := newStoreTestRequester("refresh-request")

		Expect(store.CreateAccessTokenSession(ctx, "access-signature", requester)).To(Succeed())
		Expect(store.CreateRefreshTokenSession(ctx, "refresh-signature", "access-signature", requester)).To(Succeed())
		loaded, err := store.GetRefreshTokenSession(ctx, "refresh-signature", newFositeSession("", ""))
		Expect(err).NotTo(HaveOccurred())
		Expect(loaded.GetID()).To(Equal("refresh-request"))

		Expect(store.RotateRefreshToken(ctx, "refresh-request", "refresh-signature")).To(Succeed())
		rotated, err := store.GetRefreshTokenSession(ctx, "refresh-signature", newFositeSession("", ""))
		Expect(err).To(MatchError(fosite.ErrInactiveToken))
		Expect(rotated.GetID()).To(Equal("refresh-request"))
		_, err = store.GetAccessTokenSession(ctx, "access-signature", newFositeSession("", ""))
		Expect(err).To(MatchError(fosite.ErrNotFound))

		Expect(store.DeleteRefreshTokenSession(ctx, "refresh-signature")).To(Succeed())
		_, err = store.GetRefreshTokenSession(ctx, "refresh-signature", newFositeSession("", ""))
		Expect(err).To(MatchError(fosite.ErrNotFound))
	})

	ginkgo.It("rejects stale refresh token signatures without revoking the current token pair", func() {
		ctx := context.Background()
		store := newFositeStore()
		requester := newStoreTestRequester("refresh-request")

		Expect(store.CreateAccessTokenSession(ctx, "old-access-signature", requester)).To(Succeed())
		Expect(store.CreateRefreshTokenSession(ctx, "old-refresh-signature", "old-access-signature", requester)).To(Succeed())
		Expect(store.RotateRefreshToken(ctx, "refresh-request", "old-refresh-signature")).To(Succeed())

		Expect(store.CreateAccessTokenSession(ctx, "new-access-signature", requester)).To(Succeed())
		Expect(store.CreateRefreshTokenSession(ctx, "new-refresh-signature", "new-access-signature", requester)).To(Succeed())

		err := store.RotateRefreshToken(ctx, "refresh-request", "old-refresh-signature")
		Expect(err).To(MatchError(fosite.ErrInactiveToken))
		_, err = store.GetRefreshTokenSession(ctx, "new-refresh-signature", newFositeSession("", ""))
		Expect(err).NotTo(HaveOccurred())
		_, err = store.GetAccessTokenSession(ctx, "new-access-signature", newFositeSession("", ""))
		Expect(err).NotTo(HaveOccurred())
	})

	ginkgo.DescribeTable("isolated worker access",
		func(worker int) {
			store := newFositeStore()
			Expect(runFositeStoreWorkerRoundTrip(store, worker)).To(Succeed())
		},
		ginkgo.Entry("handles worker 0", 0),
		ginkgo.Entry("handles worker 1", 1),
		ginkgo.Entry("handles worker 2", 2),
		ginkgo.Entry("handles worker 3", 3),
		ginkgo.Entry("handles worker 4", 4),
		ginkgo.Entry("handles worker 5", 5),
		ginkgo.Entry("handles worker 6", 6),
		ginkgo.Entry("handles worker 7", 7),
	)

	ginkgo.It("keeps a shared store safe under concurrent workers", func() {
		store := newFositeStore()
		start := make(chan struct{})
		errs := make(chan error, 8)
		var wg sync.WaitGroup

		for i := 0; i < 8; i++ {
			i := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				errs <- runFositeStoreWorkerRoundTrip(store, i)
			}()
		}
		close(start)
		wg.Wait()
		close(errs)

		for err := range errs {
			Expect(err).NotTo(HaveOccurred())
		}
	})
})

func runFositeStoreWorkerRoundTrip(store *fositeStore, worker int) error {
	ctx := context.Background()
	requester := newStoreTestRequester(fmt.Sprintf("request-%d", worker))
	accessSignature := fmt.Sprintf("access-%d", worker)
	refreshSignature := fmt.Sprintf("refresh-%d", worker)

	if err := store.CreateAccessTokenSession(ctx, accessSignature, requester); err != nil {
		return fmt.Errorf("CreateAccessTokenSession: %w", err)
	}
	if err := store.CreateRefreshTokenSession(ctx, refreshSignature, accessSignature, requester); err != nil {
		return fmt.Errorf("CreateRefreshTokenSession: %w", err)
	}
	if _, err := store.GetAccessTokenSession(ctx, accessSignature, newFositeSession("", "")); err != nil {
		return fmt.Errorf("GetAccessTokenSession: %w", err)
	}
	if _, err := store.GetRefreshTokenSession(ctx, refreshSignature, newFositeSession("", "")); err != nil {
		return fmt.Errorf("GetRefreshTokenSession: %w", err)
	}
	return nil
}

func newStoreTestRequester(id string) fosite.Requester {
	requester := fosite.NewAccessRequest(newFositeSession("user-"+id, "user-"+id))
	requester.SetID(id)
	requester.Client = fixedOAuthClient().fositeClient()
	requester.GrantScope(ScopeMCP)
	return requester
}

package oauth

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ory/fosite"
)

func TestFositeStoreClientCreateAndGet(t *testing.T) {
	ctx := context.Background()
	store := newFositeStore()

	if _, err := store.GetClient(ctx, ClientID); !errors.Is(err, fosite.ErrNotFound) {
		t.Fatalf("missing client error = %v, want ErrNotFound", err)
	}

	if err := store.setClient(fixedOAuthClient()); err != nil {
		t.Fatalf("set client: %v", err)
	}
	client, err := store.GetClient(ctx, ClientID)
	if err != nil {
		t.Fatalf("GetClient failed: %v", err)
	}
	if got := client.GetID(); got != ClientID {
		t.Fatalf("client ID = %q, want %q", got, ClientID)
	}
	if !client.IsPublic() {
		t.Fatalf("client is confidential, want public")
	}
}

func TestFositeStoreAuthorizeCodeInvalidationReturnsStoredRequester(t *testing.T) {
	ctx := context.Background()
	store := newFositeStore()
	requester := newStoreTestRequester("authorize-request")

	if err := store.CreateAuthorizeCodeSession(ctx, "code-signature", requester); err != nil {
		t.Fatalf("CreateAuthorizeCodeSession failed: %v", err)
	}
	loaded, err := store.GetAuthorizeCodeSession(ctx, "code-signature", newFositeSession("", ""))
	if err != nil {
		t.Fatalf("GetAuthorizeCodeSession failed: %v", err)
	}
	if got := loaded.GetID(); got != "authorize-request" {
		t.Fatalf("loaded requester ID = %q, want authorize-request", got)
	}

	if err := store.InvalidateAuthorizeCodeSession(ctx, "code-signature"); err != nil {
		t.Fatalf("InvalidateAuthorizeCodeSession failed: %v", err)
	}
	invalidated, err := store.GetAuthorizeCodeSession(ctx, "code-signature", newFositeSession("", ""))
	if !errors.Is(err, fosite.ErrInvalidatedAuthorizeCode) {
		t.Fatalf("invalidated authorize code error = %v, want ErrInvalidatedAuthorizeCode", err)
	}
	if invalidated == nil || invalidated.GetID() != "authorize-request" {
		t.Fatalf("invalidated requester = %#v, want stored requester", invalidated)
	}
}

func TestFositeStorePKCECreateGetDelete(t *testing.T) {
	ctx := context.Background()
	store := newFositeStore()
	requester := newStoreTestRequester("pkce-request")

	if err := store.CreatePKCERequestSession(ctx, "code-signature", requester); err != nil {
		t.Fatalf("CreatePKCERequestSession failed: %v", err)
	}
	loaded, err := store.GetPKCERequestSession(ctx, "code-signature", newFositeSession("", ""))
	if err != nil {
		t.Fatalf("GetPKCERequestSession failed: %v", err)
	}
	if got := loaded.GetID(); got != "pkce-request" {
		t.Fatalf("loaded PKCE requester ID = %q, want pkce-request", got)
	}
	if err := store.DeletePKCERequestSession(ctx, "code-signature"); err != nil {
		t.Fatalf("DeletePKCERequestSession failed: %v", err)
	}
	if _, err := store.GetPKCERequestSession(ctx, "code-signature", newFositeSession("", "")); !errors.Is(err, fosite.ErrNotFound) {
		t.Fatalf("deleted PKCE error = %v, want ErrNotFound", err)
	}
}

func TestFositeStoreAccessTokenCreateGetDelete(t *testing.T) {
	ctx := context.Background()
	store := newFositeStore()
	requester := newStoreTestRequester("access-request")

	if err := store.CreateAccessTokenSession(ctx, "access-signature", requester); err != nil {
		t.Fatalf("CreateAccessTokenSession failed: %v", err)
	}
	loaded, err := store.GetAccessTokenSession(ctx, "access-signature", newFositeSession("", ""))
	if err != nil {
		t.Fatalf("GetAccessTokenSession failed: %v", err)
	}
	if got := loaded.GetID(); got != "access-request" {
		t.Fatalf("loaded access requester ID = %q, want access-request", got)
	}
	if err := store.DeleteAccessTokenSession(ctx, "access-signature"); err != nil {
		t.Fatalf("DeleteAccessTokenSession failed: %v", err)
	}
	if _, err := store.GetAccessTokenSession(ctx, "access-signature", newFositeSession("", "")); !errors.Is(err, fosite.ErrNotFound) {
		t.Fatalf("deleted access token error = %v, want ErrNotFound", err)
	}
}

func TestFositeStoreRefreshTokenCreateDeleteAndRotate(t *testing.T) {
	ctx := context.Background()
	store := newFositeStore()
	requester := newStoreTestRequester("refresh-request")

	if err := store.CreateAccessTokenSession(ctx, "access-signature", requester); err != nil {
		t.Fatalf("CreateAccessTokenSession failed: %v", err)
	}
	if err := store.CreateRefreshTokenSession(ctx, "refresh-signature", "access-signature", requester); err != nil {
		t.Fatalf("CreateRefreshTokenSession failed: %v", err)
	}
	loaded, err := store.GetRefreshTokenSession(ctx, "refresh-signature", newFositeSession("", ""))
	if err != nil {
		t.Fatalf("GetRefreshTokenSession failed: %v", err)
	}
	if got := loaded.GetID(); got != "refresh-request" {
		t.Fatalf("loaded refresh requester ID = %q, want refresh-request", got)
	}

	if err := store.RotateRefreshToken(ctx, "refresh-request", "refresh-signature"); err != nil {
		t.Fatalf("RotateRefreshToken failed: %v", err)
	}
	rotated, err := store.GetRefreshTokenSession(ctx, "refresh-signature", newFositeSession("", ""))
	if !errors.Is(err, fosite.ErrInactiveToken) {
		t.Fatalf("rotated refresh token error = %v, want ErrInactiveToken", err)
	}
	if rotated == nil || rotated.GetID() != "refresh-request" {
		t.Fatalf("rotated refresh requester = %#v, want stored requester", rotated)
	}
	if _, err := store.GetAccessTokenSession(ctx, "access-signature", newFositeSession("", "")); !errors.Is(err, fosite.ErrNotFound) {
		t.Fatalf("rotated access token error = %v, want ErrNotFound", err)
	}

	if err := store.DeleteRefreshTokenSession(ctx, "refresh-signature"); err != nil {
		t.Fatalf("DeleteRefreshTokenSession failed: %v", err)
	}
	if _, err := store.GetRefreshTokenSession(ctx, "refresh-signature", newFositeSession("", "")); !errors.Is(err, fosite.ErrNotFound) {
		t.Fatalf("deleted refresh token error = %v, want ErrNotFound", err)
	}
}

func TestFositeStoreRotateRefreshTokenRejectsStaleSignatureWithoutRevokingCurrentToken(t *testing.T) {
	ctx := context.Background()
	store := newFositeStore()
	requester := newStoreTestRequester("refresh-request")

	if err := store.CreateAccessTokenSession(ctx, "old-access-signature", requester); err != nil {
		t.Fatalf("CreateAccessTokenSession old failed: %v", err)
	}
	if err := store.CreateRefreshTokenSession(ctx, "old-refresh-signature", "old-access-signature", requester); err != nil {
		t.Fatalf("CreateRefreshTokenSession old failed: %v", err)
	}
	if err := store.RotateRefreshToken(ctx, "refresh-request", "old-refresh-signature"); err != nil {
		t.Fatalf("RotateRefreshToken old failed: %v", err)
	}

	if err := store.CreateAccessTokenSession(ctx, "new-access-signature", requester); err != nil {
		t.Fatalf("CreateAccessTokenSession new failed: %v", err)
	}
	if err := store.CreateRefreshTokenSession(ctx, "new-refresh-signature", "new-access-signature", requester); err != nil {
		t.Fatalf("CreateRefreshTokenSession new failed: %v", err)
	}

	err := store.RotateRefreshToken(ctx, "refresh-request", "old-refresh-signature")
	if !errors.Is(err, fosite.ErrInactiveToken) {
		t.Fatalf("RotateRefreshToken stale old signature error = %v, want ErrInactiveToken", err)
	}
	if _, err := store.GetRefreshTokenSession(ctx, "new-refresh-signature", newFositeSession("", "")); err != nil {
		t.Fatalf("current refresh token after stale rotate = %v, want active", err)
	}
	if _, err := store.GetAccessTokenSession(ctx, "new-access-signature", newFositeSession("", "")); err != nil {
		t.Fatalf("current access token after stale rotate = %v, want active", err)
	}
}

func TestFositeStoreConcurrentAccess(t *testing.T) {
	store := newFositeStore()

	for i := 0; i < 8; i++ {
		i := i
		t.Run(fmt.Sprintf("worker-%d", i), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			requester := newStoreTestRequester(fmt.Sprintf("request-%d", i))
			accessSignature := fmt.Sprintf("access-%d", i)
			refreshSignature := fmt.Sprintf("refresh-%d", i)

			if err := store.CreateAccessTokenSession(ctx, accessSignature, requester); err != nil {
				t.Fatalf("CreateAccessTokenSession failed: %v", err)
			}
			if err := store.CreateRefreshTokenSession(ctx, refreshSignature, accessSignature, requester); err != nil {
				t.Fatalf("CreateRefreshTokenSession failed: %v", err)
			}
			if _, err := store.GetAccessTokenSession(ctx, accessSignature, newFositeSession("", "")); err != nil {
				t.Fatalf("GetAccessTokenSession failed: %v", err)
			}
			if _, err := store.GetRefreshTokenSession(ctx, refreshSignature, newFositeSession("", "")); err != nil {
				t.Fatalf("GetRefreshTokenSession failed: %v", err)
			}
		})
	}
}

func newStoreTestRequester(id string) fosite.Requester {
	requester := fosite.NewAccessRequest(newFositeSession("user-"+id, "user-"+id))
	requester.SetID(id)
	requester.Client = fixedOAuthClient().fositeClient()
	requester.GrantScope(ScopeMCP)
	return requester
}

// Package e2eproxy contains integration tests that verify reverse-proxy
// authentication works end-to-end with a real proxy in front of LeafWiki.
// Runtime specs expect a reachable proxy URL before they are executed.
package e2eproxy

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// proxyURL is the reverse-proxy entry point (all requests go through the proxy).
var proxyURL string

// directURL is retained for tests that need a direct LeafWiki endpoint.
var directURL string

func TestE2EProxySuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Proxy Suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {
	proxyURL = envOr("E2E_PROXY_URL", "http://localhost:8095")
	directURL = envOr("E2E_DIRECT_URL", "")

	if ginkgoDryRunEnabled() {
		return
	}

	err := waitReachable(ctx, proxyURL+"/api/config", 60*time.Second)
	Expect(err).NotTo(HaveOccurred(), "LeafWiki proxy stack not reachable at %s", proxyURL)
}, NodeTimeout(65*time.Second))

func ginkgoDryRunEnabled() bool {
	f := flag.Lookup("ginkgo.dry-run")
	return f != nil && f.Value.String() == "true"
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func waitReachable(ctx context.Context, url string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lastErr error
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}

		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			resp.Body.Close()
			return nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return fmt.Errorf("timed out after %s waiting for %s: %w", timeout, url, lastErr)
			}
			return fmt.Errorf("timed out after %s waiting for %s: %w", timeout, url, ctx.Err())
		case <-time.After(500 * time.Millisecond):
		}
	}
}

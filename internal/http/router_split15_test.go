package http_test

import (
	"bytes"
	"encoding/json"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	httpinternal "github.com/perber/wiki/internal/http"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
)

var _ = Describe("HTTP router", Label("unit"), func() {
	It("omits custom stylesheet tags for whitespace-only paths", func() {

		tag := httpinternal.BuildCustomStylesheetTag("/wiki", "   ")
		Expect(tag).To(BeEmpty(), "expected empty tag for whitespace path, got %q", tag)

	})
})

var _ = DescribeTable("loopback host detection", Label("unit"),
	func(host string, want bool) {
		{

			got := httpinternal.IsLoopbackHost(host)
			Expect(got).To(Equal(want), "IsLoopbackHost(%q) = %v, want %v", host, got, want)
		}

	},
	Entry("localhost", "localhost", true),
	Entry("localhost with whitespace and uppercase", " LOCALHOST ", true),
	Entry("IPv4 loopback address", "127.0.0.1", true),
	Entry("IPv6 loopback address", "::1", true),
	Entry("bracketed IPv6 loopback address", "[::1]", true),
	Entry("non-loopback IPv4 address", "192.0.2.10", false),
	Entry("empty host", "", false),
	Entry("malformed host", "not a host", false),
)

var _ = DescribeTable("loopback remote address detection", Label("unit"),
	func(remoteAddr string, want bool) {
		{

			got := httpinternal.IsLoopbackRemoteAddr(remoteAddr)
			Expect(got).To(Equal(want), "IsLoopbackRemoteAddr(%q) = %v, want %v", remoteAddr, got, want)
		}

	},
	Entry("IPv4 host port", "127.0.0.1:8080", true),
	Entry("IPv6 host port", "[::1]:8080", true),
	Entry("localhost host port", "localhost:8080", true),
	Entry("loopback without port", "127.0.0.1", true),
	Entry("non-loopback host port", "203.0.113.5:8080", false),
	Entry("empty remote addr", "", false),
	Entry("malformed remote addr", "not a remote addr", false),
)

var _ = Describe("HTTP router", Label("integration"), func() {
	It("allows loopback requests through the local-only handler", func() {

		handler := httpinternal.LocalOnlyHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Local-Only", "allowed")
			w.WriteHeader(http.StatusNoContent)
		}))

		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		req.RemoteAddr = "127.0.0.1:3456"
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusNoContent), "LocalOnlyHandler loopback status = %d, want %d", rec.Code, http.StatusNoContent)
		{

			got := rec.Header().Get("X-Local-Only")
			Expect(got).To(Equal("allowed"), "LocalOnlyHandler did not preserve wrapped response header, got %q", got)
		}

	})
})

var _ = Describe("HTTP router", Label("integration"), func() {
	It("rejects non-loopback requests before they reach the wrapped handler", func() {

		handler := httpinternal.LocalOnlyHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Set("X-Local-Only", "called")
			w.WriteHeader(http.StatusNoContent)
		}))

		req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
		req.RemoteAddr = "203.0.113.5:3456"
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		Expect(rec).To(HaveHTTPStatus(http.StatusNotFound), "LocalOnlyHandler non-loopback status = %d, want %d", rec.Code, http.StatusNotFound)
		Expect(rec.Header()).NotTo(HaveKey("X-Local-Only"), "wrapped handler response header should not be present")

	})
})

func captureDefaultLogs() *bytes.Buffer {
	GinkgoHelper()

	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	DeferCleanup(func() {
		slog.SetDefault(previous)
	})
	return &logs
}

func jsonLogEntries(logs string) []map[string]any {
	GinkgoHelper()

	entries := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(logs), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry map[string]any
		{
			err := json.Unmarshal([]byte(line), &entry)
			Expect(err).NotTo(HaveOccurred(), "log line is not JSON: %v\n%s", err, line)
		}

		entries = append(entries, entry)
	}
	return entries
}

package auth_test

import (
	. "github.com/onsi/ginkgo/v2"

	authmw "github.com/perber/wiki/internal/http/middleware/auth"
)

var _ = It("TestParseTrustedProxies_Empty", func() {
	t := GinkgoT()
	tp, err := authmw.ParseTrustedProxies("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp.IsTrusted("127.0.0.1") {
		t.Error("empty list should trust nobody")
	}

})

var _ = It("TestParseTrustedProxies_SingleIP", func() {
	t := GinkgoT()
	tp, err := authmw.ParseTrustedProxies("127.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tp.IsTrusted("127.0.0.1") {
		t.Error("127.0.0.1 should be trusted")
	}
	if tp.IsTrusted("192.168.1.1") {
		t.Error("192.168.1.1 should not be trusted")
	}

})

var _ = It("TestParseTrustedProxies_MultipleIPs", func() {
	t := GinkgoT()
	tp, err := authmw.ParseTrustedProxies("127.0.0.1, ::1, 10.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, addr := range []string{"127.0.0.1", "::1", "10.0.0.1"} {
		if !tp.IsTrusted(addr) {
			t.Errorf("%s should be trusted", addr)
		}
	}
	if tp.IsTrusted("10.0.0.2") {
		t.Error("10.0.0.2 should not be trusted")
	}

})

var _ = It("TestParseTrustedProxies_CIDR", func() {
	t := GinkgoT()
	tp, err := authmw.ParseTrustedProxies("172.18.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tp.IsTrusted("172.18.0.1") {
		t.Error("172.18.0.1 should be trusted (within CIDR)")
	}
	if !tp.IsTrusted("172.18.255.254") {
		t.Error("172.18.255.254 should be trusted (within CIDR)")
	}
	if tp.IsTrusted("172.19.0.1") {
		t.Error("172.19.0.1 should not be trusted (outside CIDR)")
	}

})

var _ = It("TestParseTrustedProxies_Mixed", func() {
	t := GinkgoT()
	tp, err := authmw.ParseTrustedProxies("127.0.0.1, ::1, 172.18.0.0/16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tp.IsTrusted("127.0.0.1") {
		t.Error("127.0.0.1 should be trusted")
	}
	if !tp.IsTrusted("::1") {
		t.Error("::1 should be trusted")
	}
	if !tp.IsTrusted("172.18.42.10") {
		t.Error("172.18.42.10 should be trusted (CIDR)")
	}

})

var _ = It("TestParseTrustedProxies_InvalidIP", func() {
	t := GinkgoT()
	_, err := authmw.ParseTrustedProxies("not-an-ip")
	if err == nil {
		t.Error("expected error for invalid IP")
	}

})

var _ = It("TestParseTrustedProxies_InvalidCIDR", func() {
	t := GinkgoT()
	_, err := authmw.ParseTrustedProxies("999.0.0.0/8")
	if err == nil {
		t.Error("expected error for invalid CIDR")
	}

})

var _ = It("TestIsTrusted_WithPort", func() {
	t := GinkgoT()
	tp, err := authmw.ParseTrustedProxies("127.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// remoteAddr typically comes as "host:port" from net/http
	if !tp.IsTrusted("127.0.0.1:54321") {
		t.Error("127.0.0.1:54321 should be trusted (port stripped)")
	}

})

var _ = It("TestIsTrusted_InvalidAddr", func() {
	t := GinkgoT()
	tp, err := authmw.ParseTrustedProxies("127.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tp.IsTrusted("not-valid") {
		t.Error("invalid address should not be trusted")
	}

})

package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestSecuritySubjectHashDoesNotPersistSensitiveInput(t *testing.T) {
	service := &Service{jwtSecret: []byte("test-secret")}
	value := "Sensitive.User@example.test"
	got := service.securitySubjectHash(securityActionLogin, "identity", value)
	if len(got) != 64 {
		t.Fatalf("security subject hash length = %d, want 64", len(got))
	}
	if strings.Contains(strings.ToLower(got), strings.ToLower(value)) {
		t.Fatalf("security subject hash contains source value: %q", got)
	}
	if got != service.securitySubjectHash(securityActionLogin, "identity", value) {
		t.Fatal("security subject hash is not deterministic")
	}
	if got == service.securitySubjectHash(securityActionRegister, "identity", value) ||
		got == service.securitySubjectHash(securityActionLogin, "client", value) ||
		got == (&Service{jwtSecret: []byte("other-secret")}).securitySubjectHash(securityActionLogin, "identity", value) {
		t.Fatal("security subject hash is not separated by action, kind, and service secret")
	}
}

func TestClientIPIgnoresForwardingHeadersFromUntrustedPeer(t *testing.T) {
	service := &Service{}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	request.RemoteAddr = "198.51.100.25:43120"
	request.Header.Set("Forwarded", "for=203.0.113.9")
	request.Header.Set("X-Forwarded-For", "203.0.113.10")

	if got := service.clientIP(request); got != "198.51.100.25" {
		t.Fatalf("client IP = %q, want direct peer", got)
	}
}

func TestClientIPUsesFirstUntrustedHopBehindTrustedProxies(t *testing.T) {
	service := &Service{}
	if err := service.ConfigureTrustedProxies("10.0.0.0/8, 2001:db8:ffff::/48"); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	request.RemoteAddr = "10.0.0.8:43120"
	request.Header.Set("X-Forwarded-For", "198.51.100.200, 203.0.113.15, 10.0.0.7")

	if got := service.clientIP(request); got != "203.0.113.15" {
		t.Fatalf("client IP = %q, want nearest untrusted hop", got)
	}
}

func TestClientIPSupportsForwardedIPv6(t *testing.T) {
	service := &Service{}
	if err := service.ConfigureTrustedProxies("2001:db8:ffff::/48"); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", nil)
	request.RemoteAddr = "[2001:db8:ffff::10]:43120"
	request.Header.Set("Forwarded", `for="[2001:db8:1234::9]:4711";proto=https, for="[2001:db8:ffff::8]"`)

	if got := service.clientIP(request); got != "2001:db8:1234::9" {
		t.Fatalf("client IP = %q, want forwarded IPv6 client", got)
	}
}

func TestConfigureTrustedProxiesRejectsInvalidCIDR(t *testing.T) {
	service := &Service{}
	if err := service.ConfigureTrustedProxies("10.0.0.0/8,not-a-network"); err == nil {
		t.Fatal("invalid trusted proxy configuration was accepted")
	}
}

func TestPasswordMatchesUsesConstantCostPlaceholderForMissingHash(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("Password123!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	if !passwordMatches(string(hash), "Password123!") {
		t.Fatal("stored password did not match")
	}
	if passwordMatches(string(hash), "wrong-password") {
		t.Fatal("wrong stored password matched")
	}
	if passwordMatches("", "Password123!") {
		t.Fatal("missing account placeholder matched")
	}
}

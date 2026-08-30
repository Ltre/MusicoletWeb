package httpapi

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIsSecureRecognizesTLSAndForwardedHTTPS(t *testing.T) {
	tlsReq := httptest.NewRequest(http.MethodGet, "https://example.test/", nil)
	if tlsReq.TLS == nil {
		tlsReq.TLS = &tls.ConnectionState{}
	}
	if !requestIsSecure(tlsReq) {
		t.Fatal("direct TLS request must be secure")
	}

	proxyReq := httptest.NewRequest(http.MethodGet, "http://backend/", nil)
	proxyReq.Header.Set("X-Forwarded-Proto", "https")
	if !requestIsSecure(proxyReq) {
		t.Fatal("forwarded HTTPS request must be secure")
	}
	proxyReq.Header.Set("X-Forwarded-Proto", "https, http")
	if !requestIsSecure(proxyReq) {
		t.Fatal("first forwarded scheme should represent the client-facing hop")
	}
	proxyReq.Header.Set("X-Forwarded-Proto", "http")
	if requestIsSecure(proxyReq) {
		t.Fatal("plain forwarded HTTP request must not be secure")
	}
}

func TestLogoutUsesSecureCookiesBehindHTTPSProxy(t *testing.T) {
	s := &Server{}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	s.logout(rr, req)
	values := rr.Header().Values("Set-Cookie")
	if len(values) != 2 {
		t.Fatalf("Set-Cookie count=%d", len(values))
	}
	for _, v := range values {
		if !strings.Contains(v, "; Secure") {
			t.Fatalf("proxy HTTPS deletion cookie is not Secure: %q", v)
		}
		if !strings.Contains(v, "SameSite=Strict") {
			t.Fatalf("deletion cookie lost SameSite=Strict: %q", v)
		}
	}
}

package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Ltre/MusicoletWeb/internal/agenthub"
	"github.com/Ltre/MusicoletWeb/internal/auth"
	"github.com/Ltre/MusicoletWeb/internal/config"
)

func TestProtectedRoutesRejectAnonymousRequests(t *testing.T) {
	am := auth.New("admin", "password", "JBSWY3DPEHPK3PXP", "test-session-key")
	s := New(config.Config{}, nil, am, agenthub.New(), nil)
	for _, tc := range []struct {
		method, target string
	}{
		{http.MethodGet, "/api/library"},
		{http.MethodGet, "/api/playback"},
		{http.MethodGet, "/api/procedure"},
		{http.MethodGet, "/api/media?path=%2Fmusic%2Fa.mp3"},
		{http.MethodPost, "/api/song/favorite"},
		{http.MethodPost, "/api/procedure/action"},
	} {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.target, strings.NewReader(`{}`))
		s.Handler().ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status=%d body=%s", tc.method, tc.target, rr.Code, rr.Body.String())
		}
	}
}

func TestWriteRouteRequiresCSRFWithValidSession(t *testing.T) {
	am := auth.New("admin", "password", "JBSWY3DPEHPK3PXP", "test-session-key")
	s := New(config.Config{}, nil, am, agenthub.New(), nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/song/favorite", strings.NewReader(`{}`))
	req.AddCookie(&http.Cookie{Name: "mw_session", Value: am.Issue(time.Now())})
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "csrf") {
		t.Fatalf("missing CSRF rejection: %q", rr.Body.String())
	}
}

func TestHealthRemainsExplicitlyPublic(t *testing.T) {
	am := auth.New("admin", "password", "JBSWY3DPEHPK3PXP", "test-session-key")
	s := New(config.Config{}, nil, am, agenthub.New(), nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	s.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

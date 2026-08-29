package httpapi

import (
	"crypto/subtle"
	"net/http"
	"time"
)

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Username, Password, TOTP string }
	if readJSON(w, r, &in) != nil {
		return
	}
	if !s.Auth.CheckPassword(in.Username, in.Password) || !s.Auth.VerifyTOTP(in.TOTP, time.Now()) {
		writeJSON(w, 401, map[string]string{"error": "invalid credentials"})
		return
	}
	tok := s.Auth.Issue(time.Now())
	csrf := randomToken(24)
	http.SetCookie(w, &http.Cookie{Name: "mw_session", Value: tok, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
	http.SetCookie(w, &http.Cookie{Name: "mw_csrf", Value: csrf, Path: "/", HttpOnly: false, Secure: r.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: 86400})
	writeJSON(w, 200, map[string]any{"ok": true, "csrf": csrf})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "mw_session", Path: "/", MaxAge: -1, HttpOnly: true})
	http.SetCookie(w, &http.Cookie{Name: "mw_csrf", Path: "/", MaxAge: -1})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"username": s.Cfg.AdminUsername})
}

func (s *Server) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := r.Cookie("mw_session")
		if e != nil || s.Auth.VerifySession(c.Value, time.Now()) != nil {
			writeJSON(w, 401, map[string]string{"error": "auth required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) protectWrite(next http.Handler) http.Handler {
	return s.protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, e := r.Cookie("mw_csrf")
		h := r.Header.Get("X-CSRF-Token")
		if e != nil || h == "" || subtle.ConstantTimeCompare([]byte(c.Value), []byte(h)) != 1 {
			writeJSON(w, 403, map[string]string{"error": "csrf"})
			return
		}
		next.ServeHTTP(w, r)
	}))
}

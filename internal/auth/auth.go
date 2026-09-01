package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const CookieName = "musicolet_session"

type session struct {
	Username string
	CSRF     string
	Expires  time.Time
}

type Manager struct {
	username string
	password [32]byte
	totp     string
	key      []byte
	ttl      time.Duration
	dev      bool
	mu       sync.RWMutex
	sessions map[string]session
}

func New(username, password, totpSecret, sessionKey string, ttl time.Duration, dev bool) *Manager {
	return &Manager{username: username, password: sha256.Sum256([]byte(password)), totp: totpSecret,
		key: []byte(sessionKey), ttl: ttl, dev: dev, sessions: make(map[string]session)}
}

func (m *Manager) Login(username, password, code string) (token, csrf string, err error) {
	want := m.password
	got := sha256.Sum256([]byte(password))
	if subtle.ConstantTimeCompare([]byte(username), []byte(m.username)) != 1 || subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
		return "", "", errors.New("invalid credentials")
	}
	if !(m.dev && m.totp == "") && !ValidateTOTP(m.totp, code, time.Now()) {
		return "", "", errors.New("invalid authenticator code")
	}
	raw := randomToken(32)
	csrf = randomToken(24)
	exp := time.Now().Add(m.ttl)
	payload := raw + "." + strconv.FormatInt(exp.Unix(), 10)
	token = payload + "." + m.sign(payload)
	m.mu.Lock()
	m.sessions[raw] = session{Username: username, CSRF: csrf, Expires: exp}
	m.mu.Unlock()
	return token, csrf, nil
}

func (m *Manager) Authenticate(r *http.Request) (username, csrf string, ok bool) {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return "", "", false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 3 {
		return "", "", false
	}
	payload := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(parts[2]), []byte(m.sign(payload))) {
		return "", "", false
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || time.Now().Unix() >= exp {
		m.delete(parts[0])
		return "", "", false
	}
	m.mu.RLock()
	s, exists := m.sessions[parts[0]]
	m.mu.RUnlock()
	if !exists || time.Now().After(s.Expires) {
		return "", "", false
	}
	return s.Username, s.CSRF, true
}

func (m *Manager) Logout(r *http.Request) {
	if cookie, err := r.Cookie(CookieName); err == nil {
		parts := strings.Split(cookie.Value, ".")
		if len(parts) > 0 {
			m.delete(parts[0])
		}
	}
}

func (m *Manager) sign(payload string) string {
	h := hmac.New(sha256.New, m.key)
	h.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func (m *Manager) delete(id string) { m.mu.Lock(); delete(m.sessions, id); m.mu.Unlock() }

func randomToken(size int) string {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// ValidateTOTP implements RFC 6238 using SHA-1, a 30-second step and six digits.
func ValidateTOTP(secret, code string, now time.Time) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", "")))
	if err != nil || len(key) == 0 {
		return false
	}
	for drift := int64(-1); drift <= 1; drift++ {
		counter := uint64(now.Unix()/30 + drift)
		var buf [8]byte
		binary.BigEndian.PutUint64(buf[:], counter)
		h := hmac.New(sha1.New, key)
		h.Write(buf[:])
		sum := h.Sum(nil)
		offset := sum[len(sum)-1] & 0x0f
		value := (uint32(sum[offset])&0x7f)<<24 | uint32(sum[offset+1])<<16 | uint32(sum[offset+2])<<8 | uint32(sum[offset+3])
		if subtle.ConstantTimeCompare([]byte(fmt.Sprintf("%06d", value%1_000_000)), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

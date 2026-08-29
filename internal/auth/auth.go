package auth

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Manager struct {
	Username, Password, TOTPSecret string
	sessionKey                     []byte
	TTL                            time.Duration
}

func New(username, password, secret, sessionKey string) *Manager {
	return &Manager{Username: username, Password: password, TOTPSecret: secret, sessionKey: []byte(sessionKey), TTL: 24 * time.Hour}
}
func (m *Manager) CheckPassword(user, pass string) bool {
	return subtle.ConstantTimeCompare([]byte(user), []byte(m.Username)) == 1 && subtle.ConstantTimeCompare([]byte(pass), []byte(m.Password)) == 1
}
func (m *Manager) VerifyTOTP(code string, now time.Time) bool {
	for d := -1; d <= 1; d++ {
		if totp(m.TOTPSecret, now.Add(time.Duration(d)*30*time.Second)) == code {
			return true
		}
	}
	return false
}
func totp(secret string, t time.Time) string {
	clean := strings.TrimRight(strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", "")), "=")
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(clean)
	if err != nil {
		return ""
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(t.Unix()/30))
	h := hmac.New(sha1.New, key)
	h.Write(msg[:])
	sum := h.Sum(nil)
	o := sum[len(sum)-1] & 0x0f
	n := binary.BigEndian.Uint32(sum[o:o+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", n%1000000)
}
func (m *Manager) Issue(now time.Time) string {
	exp := now.Add(m.TTL).Unix()
	payload := strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, m.sessionKey)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func (m *Manager) VerifySession(token string, now time.Time) error {
	p := strings.Split(token, ".")
	if len(p) != 2 {
		return errors.New("bad session")
	}
	raw, err := base64.RawURLEncoding.DecodeString(p[0])
	if err != nil {
		return err
	}
	exp, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return err
	}
	if now.Unix() > exp {
		return errors.New("expired")
	}
	sig, err := base64.RawURLEncoding.DecodeString(p[1])
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, m.sessionKey)
	mac.Write(raw)
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return errors.New("bad signature")
	}
	return nil
}

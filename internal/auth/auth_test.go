package auth

import (
	"testing"
	"time"
)

func TestTOTPVerify(t *testing.T) {
	m := New("admin", "pw", "JBSWY3DPEHPK3PXP", "session")
	now := time.Unix(1700000000, 0)
	code := totp(m.TOTPSecret, now)
	if len(code) != 6 || !m.VerifyTOTP(code, now) {
		t.Fatal("totp verification failed")
	}
	if m.VerifyTOTP("000000", now) && code != "000000" {
		t.Fatal("unexpected totp acceptance")
	}
}
func TestSession(t *testing.T) {
	m := New("a", "b", "JBSWY3DPEHPK3PXP", "k")
	now := time.Unix(1700000000, 0)
	tok := m.Issue(now)
	if e := m.VerifySession(tok, now.Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	if e := m.VerifySession(tok, now.Add(25*time.Hour)); e == nil {
		t.Fatal("expected expiry")
	}
}

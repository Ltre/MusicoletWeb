package securestore

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"testing"
)

func TestCipherRoundTripPrimitive(t *testing.T) {
	key := sha256.Sum256([]byte("12345678901234567890123456789012"))
	b, err := aes.NewCipher(key[:])
	if err != nil {
		t.Fatal(err)
	}
	g, err := cipher.NewGCM(b)
	if err != nil {
		t.Fatal(err)
	}
	nonce := bytes.Repeat([]byte{7}, g.NonceSize())
	ct := g.Seal(nil, nonce, []byte("secret"), []byte("agent_token"))
	pt, err := g.Open(nil, nonce, ct, []byte("agent_token"))
	if err != nil {
		t.Fatal(err)
	}
	if string(pt) != "secret" {
		t.Fatal(string(pt))
	}
}

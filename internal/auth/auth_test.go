package auth

import (
	"encoding/base32"
	"fmt"
	"testing"
	"time"
)

func TestValidateTOTP(t *testing.T) {
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte("12345678901234567890"))
	at := time.Unix(59, 0)
	if !ValidateTOTP(secret, "287082", at) {
		t.Fatal("RFC 6238 compatible code was rejected")
	}
	if ValidateTOTP(secret, "000000", at) {
		t.Fatal("invalid code was accepted")
	}
}

func ExampleValidateTOTP() {
	fmt.Println(ValidateTOTP("JBSWY3DPEHPK3PXP", "000000", time.Unix(0, 0))) // Output: false
}

package server

import (
	"testing"
	"time"
)

func TestTOTPRoundTrip(t *testing.T) {
	secret, err := generateTOTPSecret()
	if err != nil {
		t.Fatalf("generateTOTPSecret: %v", err)
	}
	if len(secret) != 32 { // 20 bytes base32-nopad = 32 chars
		t.Fatalf("secret length = %d, want 32", len(secret))
	}
	now := time.Now()
	code, err := totpAt(secret, now)
	if err != nil {
		t.Fatalf("totpAt: %v", err)
	}
	if len(code) != totpDigits {
		t.Fatalf("code %q length = %d, want %d", code, len(code), totpDigits)
	}
	if !verifyTOTP(secret, code) {
		t.Error("current code rejected")
	}

	// previous/next period accepted within ±1 skew
	prev, _ := totpAt(secret, now.Add(-totpPeriod*time.Second))
	if !verifyTOTP(secret, prev) {
		t.Error("previous-period code should pass within skew")
	}
	next, _ := totpAt(secret, now.Add(totpPeriod*time.Second))
	if !verifyTOTP(secret, next) {
		t.Error("next-period code should pass within skew")
	}

	// two periods away rejected
	far, _ := totpAt(secret, now.Add(3*totpPeriod*time.Second))
	if verifyTOTP(secret, far) {
		t.Error("far code should be rejected")
	}

	// malformed input rejected
	if verifyTOTP(secret, "12345") || verifyTOTP(secret, "") || verifyTOTP("", code) {
		t.Error("malformed input should be rejected")
	}
}

func TestTOTPURI(t *testing.T) {
	uri := totpURI("ABC234", "MusicWeb", "admin")
	if uri == "" || uri[:15] != "otpauth://totp/" {
		t.Fatalf("unexpected uri: %q", uri)
	}
	if _, err := totpQRDataURI(uri); err != nil {
		t.Fatalf("totpQRDataURI: %v", err)
	}
}

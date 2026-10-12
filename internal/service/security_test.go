package service

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordHashAndVerification(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("correct horse battery staple", hash) {
		t.Fatal("expected valid password")
	}
	if VerifyPassword("incorrect", hash) {
		t.Fatal("accepted incorrect password")
	}
}

func TestTokenSignerExpiryAndTamper(t *testing.T) {
	signer := TokenSigner{Secret: []byte(strings.Repeat("s", 32))}
	expires := time.Now().Add(time.Minute)
	token, err := signer.Issue("session-id", 7, 3, expires)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := signer.Verify(token, time.Now())
	if err != nil || claims.UserID != 7 || claims.TeamID != 3 {
		t.Fatalf("verify claims = %#v, %v", claims, err)
	}
	if _, err = signer.Verify(token+"x", time.Now()); err == nil {
		t.Fatal("accepted modified token")
	}
	if _, err = signer.Verify(token, expires); err == nil {
		t.Fatal("accepted expired token")
	}
}

func TestSealSecretUsesRandomizedCiphertext(t *testing.T) {
	first, err := SealSecret("application secret used for test", "sensitive-value")
	if err != nil {
		t.Fatal(err)
	}
	second, err := SealSecret("application secret used for test", "sensitive-value")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || strings.Contains(first, "sensitive-value") {
		t.Fatal("secret ciphertext is not randomized or leaked plaintext")
	}
	plain, err := OpenSecret("application secret used for test", first)
	if err != nil || plain != "sensitive-value" {
		t.Fatalf("OpenSecret = %q, %v", plain, err)
	}
}

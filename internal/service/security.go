// Package service implements application use cases independent of HTTP.
package service

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/pbkdf2"
)

// SealSecret encrypts credentials for database storage using a key derived from
// APP_SECRET. It uses a fresh nonce on every call and returns an opaque value.
func SealSecret(secret, value string) (string, error) {
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := aead.Seal(nonce, nonce, []byte(value), nil)
	return "enc:v1:" + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// OpenSecret decrypts a credential stored by SealSecret.
func OpenSecret(secret, value string) (string, error) {
	if !strings.HasPrefix(value, "enc:v1:") {
		return "", errors.New("credential format is not encrypted")
	}
	key := sha256.Sum256([]byte(secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return "", err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	sealed, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "enc:v1:"))
	if err != nil {
		return "", err
	}
	if len(sealed) < aead.NonceSize() {
		return "", errors.New("credential ciphertext is too short")
	}
	nonce, ciphertext := sealed[:aead.NonceSize()], sealed[aead.NonceSize():]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt credential: %w", err)
	}
	return string(plaintext), nil
}

const passwordRounds = 310_000

// HashPassword is compatible with existing Python PBKDF2-SHA256 hashes.
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	digest := pbkdf2.Key([]byte(password), salt, passwordRounds, 32, sha256.New)
	return "pbkdf2_sha256$" + base64.URLEncoding.EncodeToString(salt) + "$" +
		base64.URLEncoding.EncodeToString(digest), nil
}

// VerifyPassword validates current Python and Go password hash records.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 3 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	salt, err := base64.URLEncoding.DecodeString(parts[1])
	if err != nil || len(salt) < 16 {
		return false
	}
	expected, err := base64.URLEncoding.DecodeString(parts[2])
	if err != nil || len(expected) != 32 {
		return false
	}
	actual := pbkdf2.Key([]byte(password), salt, passwordRounds, 32, sha256.New)
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

type tokenPayload struct {
	SessionID string `json:"sid"`
	UserID    int64  `json:"sub"`
	TeamID    int64  `json:"tid"`
	ExpiresAt int64  `json:"exp"`
}

// TokenSigner signs and verifies the established application bearer format.
type TokenSigner struct {
	Secret []byte
}

// Issue produces a two-week HMAC token compatible with existing sessions.
func (s TokenSigner) Issue(
	sessionID string,
	userID int64,
	teamID int64,
	expires time.Time,
) (string, error) {
	payload, err := json.Marshal(tokenPayload{
		SessionID: sessionID,
		UserID:    userID,
		TeamID:    teamID,
		ExpiresAt: expires.Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("encode session token: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.Secret)
	_, _ = mac.Write([]byte(encoded))
	return encoded + "." + hex.EncodeToString(mac.Sum(nil)), nil
}

// Verify checks a token signature, expiration and payload structure.
func (s TokenSigner) Verify(token string, now time.Time) (tokenPayload, error) {
	var claims tokenPayload
	parts := strings.Split(token, ".")
	if len(parts) != 2 || len(s.Secret) < 32 {
		return claims, errors.New("invalid session token")
	}
	signature, err := hex.DecodeString(parts[1])
	if err != nil {
		return claims, errors.New("invalid session token")
	}
	mac := hmac.New(sha256.New, s.Secret)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return claims, errors.New("invalid session token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(payload, &claims) != nil {
		return claims, errors.New("invalid session token")
	}
	if claims.SessionID == "" || claims.UserID < 1 || claims.TeamID < 1 ||
		claims.ExpiresAt <= now.Unix() {
		return claims, errors.New("expired session token")
	}
	return claims, nil
}

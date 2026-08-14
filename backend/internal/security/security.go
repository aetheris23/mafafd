package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// ---------- Hashing ----------

// SHA256Hex returns a lowercase hex SHA-256 digest.
func SHA256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ---------- API Keys ----------

// GenerateAPIKey creates a secret with a recognizable prefix, e.g. sk_live_<random>.
func GenerateAPIKey() (plain, prefix, hash string, err error) {
	rb := make([]byte, 32)
	if _, err := rand.Read(rb); err != nil {
		return "", "", "", fmt.Errorf("generate key: %w", err)
	}
	randPart := strings.ToLower(hex.EncodeToString(rb))
	plain = "sk_live_" + randPart
	prefix = plain[:12]
	hash = SHA256Hex(plain)
	return plain, prefix, hash, nil
}

// ---------- Passwords ----------

// HashPassword returns a bcrypt hash (cost 12).
func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(b), nil
}

// CheckPassword verifies a plaintext password against a bcrypt hash.
func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// ---------- PII Masking (before external AI calls / storage) ----------

// MaskEmail irreversibly anonymizes an email address.
func MaskEmail(email string) string {
	if email == "" {
		return ""
	}
	return SHA256Hex(strings.ToLower(strings.TrimSpace(email)))
}

// MaskID irreversibly anonymizes a free-form identifier (customer id, fingerprint).
func MaskID(id string) string {
	if id == "" {
		return ""
	}
	return SHA256Hex(id)
}

// MaskCard irreversibly anonymizes a card token while keeping a stable key.
func MaskCard(token string) string {
	if token == "" {
		return ""
	}
	h := SHA256Hex(token)
	return "card_" + h[:24]
}

// DisplayCard returns a short, reversible-for-nobody display hint.
func DisplayCard(token string) string {
	if token == "" {
		return ""
	}
	h := SHA256Hex(token)
	return strings.ToUpper("••••" + h[len(h)-4:])
}

// MaskIP partially hides an IPv4 address for external disclosure.
func MaskIP(ip string) string {
	if ip == "" {
		return ""
	}
	if parts := strings.Split(ip, "."); len(parts) == 4 {
		return fmt.Sprintf("%s.%s.%s.x", parts[0], parts[1], parts[2])
	}
	return "::/masked"
}

// ---------- Request Signing ----------

// SignHMAC signs a payload with an HMAC-SHA256 using the given secret.
func SignHMAC(secret []byte, payload []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyHMAC does a constant-time comparison of a presented signature.
func VerifyHMAC(secret []byte, payload []byte, provided string) bool {
	expected := SignHMAC(secret, payload)
	return hmac.Equal([]byte(expected), []byte(provided))
}

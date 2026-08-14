package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestJWTIssueAndParse(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef0123456789abcdef")
	uid := uuid.New()
	tid := uuid.New()

	token, err := Sign(secret, uid, tid, "admin", "Acme", 1*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := Parse(secret, token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != uid || claims.TenantID != tid || claims.Role != "admin" {
		t.Fatalf("claims mismatch: %+v", claims)
	}
	if claims.TenantName != "Acme" {
		t.Fatalf("tenant name mismatch: %s", claims.TenantName)
	}
}

func TestJWTTamperedRejected(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef0123456789abcdef")
	uid := uuid.New()
	token, err := Sign(secret, uid, uuid.New(), "admin", "Acme", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse([]byte("different-secret-0123456789abcdef0123456789abcdef"), token); err == nil {
		t.Fatal("token signed with different secret accepted")
	}
	// corrupt the payload
	bad := token[:len(token)-4] + "AAAA"
	if _, err := Parse(secret, bad); err == nil {
		t.Fatal("tampered token accepted")
	}
}

func TestJWTExpiredRejected(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef0123456789abcdef")
	token, err := Sign(secret, uuid.New(), uuid.New(), "analyst", "Acme", -1*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(secret, token); err == nil {
		t.Fatal("expired token accepted")
	}
}

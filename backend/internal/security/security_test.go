package security

import (
	"testing"
)

func TestGenerateAPIKey(t *testing.T) {
	plain, prefix, hash, err := GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(plain) < 40 {
		t.Fatalf("key too short: %q", plain)
	}
	if len(hash) != 64 {
		t.Fatalf("hash not sha256 hex: %q", hash)
	}
	if SHA256Hex(plain) != hash {
		t.Fatal("hash does not match key")
	}
	if prefix != plain[:12] {
		t.Fatal("prefix mismatch")
	}
}

func TestAPIKeyIsOneWay(t *testing.T) {
	plain, _, hash, _ := GenerateAPIKey()
	if SHA256Hex(plain+"x") == hash {
		t.Fatal("tampered key should not hash to same value")
	}
}

func TestPasswordHashRoundTrip(t *testing.T) {
	h, err := HashPassword("s3cr3t-pass")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "s3cr3t-pass") {
		t.Fatal("correct password rejected")
	}
	if CheckPassword(h, "wrong-pass") {
		t.Fatal("wrong password accepted")
	}
}

func TestHMACVerify(t *testing.T) {
	secret := []byte("test-secret")
	payload := []byte(`{"amount":100}`)
	sig := SignHMAC(secret, payload)
	if !VerifyHMAC(secret, payload, sig) {
		t.Fatal("valid signature rejected")
	}
	if VerifyHMAC(secret, payload, "deadbeef") {
		t.Fatal("invalid signature accepted")
	}
	if VerifyHMAC(secret, []byte(`{"amount":999}`), sig) {
		t.Fatal("tampered payload accepted")
	}
}

func TestMasking(t *testing.T) {
	email := "John.Doe@Example.com"
	h1 := MaskEmail(email)
	h2 := MaskEmail("john.doe@example.com")
	if h1 != h2 {
		t.Fatal("email masking should be case-insensitive")
	}
	if len(h1) != 64 {
		t.Fatal("email hash length")
	}

	card := "4111 1111 1111 1234"
	c := MaskCard(card)
	if c == card || len(c) != len("card_")+24 {
		t.Fatalf("card not properly masked: %q", c)
	}
	if MaskCard(card) != c {
		t.Fatal("card masking must be stable for velocity checks")
	}

	ip := "203.0.113.7"
	maskedIP := MaskIP(ip)
	if maskedIP != "203.0.113.x" {
		t.Fatalf("ip mask wrong: %q", maskedIP)
	}
	if maskedIP == ip {
		t.Fatal("ip not masked")
	}
}

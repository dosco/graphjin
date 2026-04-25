package issuer

import (
	"testing"
	"time"

	"github.com/dosco/graphjin/auth/v3/provider"
	jwt "github.com/golang-jwt/jwt/v5"
)

// TestMintAndVerifyRoundtrip confirms a token we mint round-trips through the
// same generic provider the server already uses for verification.
func TestMintAndVerifyRoundtrip(t *testing.T) {
	const secret = "a-very-long-test-secret-not-for-prod-use"
	iss, err := New(Config{
		Secret:   secret,
		Issuer:   "https://graphjin.test",
		Audience: "graphjin-cli",
		TTL:      time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}

	tok, err := iss.Mint(jwt.MapClaims{
		"sub":   "https://accounts.google.com#1234",
		"email": "alice@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	p, err := provider.NewGenericProvider(provider.JWTConfig{
		Secret:   secret,
		Issuer:   "https://graphjin.test",
		Audience: "graphjin-cli",
	})
	if err != nil {
		t.Fatal(err)
	}

	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(tok, claims, p.KeyFunc())
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !parsed.Valid {
		t.Fatal("token reported invalid")
	}
	if !p.VerifyAudience(claims) {
		t.Error("audience mismatch")
	}
	if !p.VerifyIssuer(claims) {
		t.Error("issuer mismatch")
	}
	if got, _ := claims["sub"].(string); got != "https://accounts.google.com#1234" {
		t.Errorf("sub = %q", got)
	}
	if got, _ := claims["email"].(string); got != "alice@example.com" {
		t.Errorf("email = %q", got)
	}
	if _, ok := claims["iat"]; !ok {
		t.Error("iat not set")
	}
	if _, ok := claims["exp"]; !ok {
		t.Error("exp not set")
	}
}

func TestMintRejectsEmptyConfig(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("expected error for empty config")
	}
}

func TestMintHonorsExplicitClaims(t *testing.T) {
	iss, err := New(Config{Secret: "x-very-long-secret-for-testing-only", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	custom := int64(9999999999)
	tok, err := iss.Mint(jwt.MapClaims{"exp": custom, "sub": "u1"})
	if err != nil {
		t.Fatal(err)
	}

	claims := jwt.MapClaims{}
	_, err = jwt.ParseWithClaims(tok, claims, func(*jwt.Token) (interface{}, error) {
		return []byte("x-very-long-secret-for-testing-only"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := claims["exp"].(float64)
	if int64(got) != custom {
		t.Errorf("exp = %v, want %d", got, custom)
	}
}

package auth

import "testing"

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("s3cret-pass")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == "s3cret-pass" {
		t.Fatal("hash must not equal plaintext")
	}
	if !CheckPassword(hash, "s3cret-pass") {
		t.Fatal("expected correct password to verify")
	}
	if CheckPassword(hash, "wrong-pass") {
		t.Fatal("expected wrong password to fail")
	}
}

func TestHashPasswordCost(t *testing.T) {
	// bcrypt output must be 60 chars.
	hash, err := HashPassword("x")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if len(hash) != 60 {
		t.Fatalf("expected 60-char bcrypt hash, got %d", len(hash))
	}
}

func TestNewTokenUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		tok, err := NewToken()
		if err != nil {
			t.Fatalf("NewToken: %v", err)
		}
		if len(tok) < 32 {
			t.Fatalf("token too short: %d", len(tok))
		}
		if seen[tok] {
			t.Fatal("duplicate token generated")
		}
		seen[tok] = true
	}
}

func TestHashTokenIsDeterministicDigest(t *testing.T) {
	a := HashToken("abc123")
	b := HashToken("abc123")
	if a != b {
		t.Fatal("same input must hash identically")
	}
	if HashToken("abc123") == HashToken("abc124") {
		t.Fatal("different inputs must differ")
	}
	if a == "abc123" {
		t.Fatal("token must be hashed, not stored raw")
	}
}

func TestConstantTimeEqual(t *testing.T) {
	if !ConstantTimeEqual("token", "token") {
		t.Fatal("equal strings must compare true")
	}
	if ConstantTimeEqual("token", "tokem") {
		t.Fatal("different strings must compare false")
	}
	if ConstantTimeEqual("", "x") || ConstantTimeEqual("x", "") {
		t.Fatal("empty vs non-empty must compare false")
	}
}

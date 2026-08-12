package mcp

import "testing"

func TestCryptoRoundTripAndTamper(t *testing.T) {
	plain := []byte(`{"servers":{"x":{}}}`)
	a, err := EncryptPayload("pass", plain)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DecryptPayload("pass", a)
	if err != nil || string(b) != string(plain) {
		t.Fatalf("round trip: %q, %v", b, err)
	}
	if _, err := DecryptPayload("wrong", a); err == nil {
		t.Fatal("wrong password accepted")
	}
	a[len(a)-2] ^= 1
	if _, err := DecryptPayload("pass", a); err == nil {
		t.Fatal("tampered payload accepted")
	}
}

func TestCryptoUsesFreshRandomness(t *testing.T) {
	a, err := EncryptPayload("pass", []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := EncryptPayload("pass", []byte("same"))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) == string(b) {
		t.Fatal("identical ciphertexts")
	}
}

func TestCryptoRejectsMalformedAndOversized(t *testing.T) {
	if _, err := DecryptPayload("pass", []byte("{}")); err == nil {
		t.Fatal("malformed envelope accepted")
	}
	tooLarge := make([]byte, maxPayloadBytes+1)
	if _, err := EncryptPayload("pass", tooLarge); err == nil {
		t.Fatal("oversized payload accepted")
	}
}

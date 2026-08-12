package mcp

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	cryptoVersion    = 1
	cryptoIterations = 120000
	maxPayloadBytes  = 4 << 20
)

type encryptedPayload struct {
	Version    int    `json:"version"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

func EncryptPayload(password string, plaintext []byte) ([]byte, error) {
	if password == "" {
		return nil, errors.New("password is required")
	}
	if len(plaintext) > maxPayloadBytes {
		return nil, errors.New("payload exceeds size limit")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key := deriveKey([]byte(password), salt, cryptoIterations)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	e := encryptedPayload{cryptoVersion, cryptoIterations, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(nonce), base64.RawStdEncoding.EncodeToString(ciphertext)}
	return json.Marshal(e)
}

func DecryptPayload(password string, payload []byte) ([]byte, error) {
	if password == "" {
		return nil, errors.New("password is required")
	}
	var e encryptedPayload
	if err := json.Unmarshal(payload, &e); err != nil {
		return nil, errors.New("invalid encrypted payload")
	}
	if e.Version != cryptoVersion || e.Iterations < 10000 || e.Iterations > 500000 {
		return nil, errors.New("unsupported encrypted payload")
	}
	salt, err := base64.RawStdEncoding.DecodeString(e.Salt)
	if err != nil || len(salt) < 8 {
		return nil, errors.New("invalid salt")
	}
	nonce, err := base64.RawStdEncoding.DecodeString(e.Nonce)
	if err != nil {
		return nil, errors.New("invalid nonce")
	}
	ciphertext, err := base64.RawStdEncoding.DecodeString(e.Ciphertext)
	if err != nil || len(ciphertext) > maxPayloadBytes+64 {
		return nil, errors.New("invalid ciphertext")
	}
	block, err := aes.NewCipher(deriveKey([]byte(password), salt, e.Iterations))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("invalid nonce")
	}
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errors.New("encrypted payload authentication failed")
	}
	if len(plain) > maxPayloadBytes {
		return nil, errors.New("payload exceeds size limit")
	}
	return plain, nil
}

func deriveKey(password, salt []byte, iterations int) []byte {
	out := make([]byte, 32)
	for block := uint32(1); block <= 1; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		copy(out, t)
	}
	return out
}

// Package secure holds password hashing, random tokens and at-rest encryption.
package secure

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    = 2
	argonMemory  = 19 * 1024 // KiB, OWASP minimum for argon2id
	argonThreads = 1
	argonKeyLen  = 32
)

// HashPassword returns an encoded argon2id hash.
func HashPassword(password string) string {
	salt := make([]byte, 16)
	rand.Read(salt)
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key))
}

// CheckPassword compares a password with a hash made by HashPassword.
func CheckPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// Token returns n random bytes as URL-safe text.
func Token(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken is the lookup hash stored for sessions and API keys.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Box encrypts provider secrets at rest with AES-256-GCM.
type Box struct{ aead cipher.AEAD }

// NewBox derives the key from secret, or from <dataDir>/secret.key (created on first run).
func NewBox(secret, dataDir string) (*Box, error) {
	if secret == "" {
		path := filepath.Join(dataDir, "secret.key")
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			secret = Token(32)
			if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		} else {
			secret = strings.TrimSpace(string(b))
		}
	}
	key := sha256.Sum256([]byte("lowlex:" + secret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead}, nil
}

func (b *Box) Seal(plain []byte) string {
	nonce := make([]byte, b.aead.NonceSize())
	rand.Read(nonce)
	return base64.StdEncoding.EncodeToString(b.aead.Seal(nonce, nonce, plain, nil))
}

func (b *Box) Open(sealed string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil || len(raw) < b.aead.NonceSize() {
		return nil, errors.New("invalid sealed value")
	}
	n := b.aead.NonceSize()
	return b.aead.Open(nil, raw[:n], raw[n:], nil)
}

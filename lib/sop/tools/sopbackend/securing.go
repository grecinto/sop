package sopbackend

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

const secureStringPrefix = "enc:v1:"

// SecuredString encrypts its value when persisted to JSON and decrypts it when loaded.
type SecuredString string

func NewSecuredString(value string) SecuredString {
	return SecuredString(value)
}

func (s SecuredString) Plaintext() string {
	return string(s)
}

func (s SecuredString) String() string {
	return string(s)
}

func (s SecuredString) MarshalJSON() ([]byte, error) {
	value, err := s.encryptedValue()
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func (s *SecuredString) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == "" {
		*s = ""
		return nil
	}
	plaintext, err := decryptSecuredString(raw)
	if err != nil {
		return err
	}
	*s = SecuredString(plaintext)
	return nil
}

func (s SecuredString) encryptedValue() (string, error) {
	if s == "" {
		return "", nil
	}

	block, err := newEncryptionBlock()
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(s), nil)
	return secureStringPrefix + base64.StdEncoding.EncodeToString(ciphertext), nil
}

func decryptSecuredString(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if !strings.HasPrefix(value, secureStringPrefix) {
		return value, nil
	}

	encoded := strings.TrimPrefix(value, secureStringPrefix)
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("decode secured string: %w", err)
	}

	block, err := newEncryptionBlock()
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, encrypted := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, encrypted, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secured string: %w", err)
	}
	return string(plaintext), nil
}

func newEncryptionBlock() (cipher.Block, error) {
	key := configEncryptionKey()
	if len(key) == 0 {
		return nil, fmt.Errorf("SOP_CONFIG_ENCRYPTION_KEY is required for encrypted config persistence")
	}
	return aes.NewCipher(key)
}

func configEncryptionKey() []byte {
	if key := strings.TrimSpace(os.Getenv("SOP_CONFIG_ENCRYPTION_KEY")); key != "" {
		sum := sha256.Sum256([]byte(key))
		return sum[:]
	}
	return nil
}

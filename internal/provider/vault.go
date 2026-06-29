package provider

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	vaultBlobPrefix = "cwcred:v1:"
	devVaultKeySeed = "cineweave-development-credential-master-key"
)

type Vault struct {
	key []byte
}

func NewVaultFromEnv() (*Vault, error) {
	return NewVault(os.Getenv("CINEWEAVE_CREDENTIAL_MASTER_KEY"))
}

func NewVault(rawKey string) (*Vault, error) {
	key, err := normalizeVaultKey(rawKey)
	if err != nil {
		return nil, err
	}
	return &Vault{key: key}, nil
}

func (v *Vault) EncryptJSON(payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return v.Encrypt(raw)
}

func (v *Vault) Encrypt(plaintext []byte) ([]byte, error) {
	if v == nil || len(v.key) != 32 {
		return nil, errors.New("credential vault is not initialized")
	}
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, nil)
	blob := make([]byte, 0, len(vaultBlobPrefix)+len(nonce)+len(ciphertext))
	blob = append(blob, []byte(vaultBlobPrefix)...)
	blob = append(blob, nonce...)
	blob = append(blob, ciphertext...)
	return blob, nil
}

func (v *Vault) Decrypt(blob []byte) ([]byte, error) {
	if v == nil || len(v.key) != 32 {
		return nil, errors.New("credential vault is not initialized")
	}
	if !bytes.HasPrefix(blob, []byte(vaultBlobPrefix)) {
		return nil, errors.New("unsupported credential payload format")
	}
	block, err := aes.NewCipher(v.key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	payload := blob[len(vaultBlobPrefix):]
	if len(payload) < aead.NonceSize() {
		return nil, errors.New("credential payload is truncated")
	}
	nonce := payload[:aead.NonceSize()]
	ciphertext := payload[aead.NonceSize():]
	return aead.Open(nil, nonce, ciphertext, nil)
}

func MaskCredentialPayload(payload map[string]any) string {
	for _, key := range []string{"apiKey", "api_key", "token", "accessToken", "password", "secret"} {
		if value, ok := payload[key]; ok {
			if text, ok := value.(string); ok {
				return MaskSecret(text)
			}
		}
	}
	for _, value := range payload {
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			return MaskSecret(text)
		}
	}
	return "****"
}

func MaskSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return "****"
	}
	if len(secret) <= 4 {
		return "****"
	}
	if len(secret) <= 8 {
		return fmt.Sprintf("%s****%s", secret[:1], secret[len(secret)-1:])
	}
	prefixLen := 3
	if strings.HasPrefix(secret, "sk-") && len(secret) >= 7 {
		prefixLen = 3
	}
	return fmt.Sprintf("%s****%s", secret[:prefixLen], secret[len(secret)-4:])
}

func normalizeVaultKey(rawKey string) ([]byte, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		sum := sha256.Sum256([]byte(devVaultKeySeed))
		return sum[:], nil
	}
	if strings.HasPrefix(rawKey, "base64:") {
		return decodeFixedKey(rawKey[len("base64:"):], base64.StdEncoding.DecodeString)
	}
	if strings.HasPrefix(rawKey, "hex:") {
		return decodeFixedKey(rawKey[len("hex:"):], hex.DecodeString)
	}
	if len(rawKey) == 64 {
		if key, err := decodeFixedKey(rawKey, hex.DecodeString); err == nil {
			return key, nil
		}
	}
	if key, err := decodeFixedKey(rawKey, base64.StdEncoding.DecodeString); err == nil {
		return key, nil
	}
	if len([]byte(rawKey)) == 32 {
		return []byte(rawKey), nil
	}
	sum := sha256.Sum256([]byte(rawKey))
	return sum[:], nil
}

func decodeFixedKey(raw string, decode func(string) ([]byte, error)) ([]byte, error) {
	key, err := decode(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("credential master key must decode to 32 bytes, got %d", len(key))
	}
	return key, nil
}

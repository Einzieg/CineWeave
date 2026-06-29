package provider

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestVaultEncryptDecryptJSON(t *testing.T) {
	vault, err := NewVault("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("NewVault() error = %v", err)
	}

	payload := map[string]any{"apiKey": "sk-test-secret-value"}
	encrypted, err := vault.EncryptJSON(payload)
	if err != nil {
		t.Fatalf("EncryptJSON() error = %v", err)
	}
	if bytes.Contains(encrypted, []byte("sk-test-secret-value")) {
		t.Fatal("encrypted payload contains plaintext secret")
	}

	decrypted, err := vault.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(decrypted, &decoded); err != nil {
		t.Fatalf("decrypted payload is invalid JSON: %v", err)
	}
	if decoded["apiKey"] != "sk-test-secret-value" {
		t.Fatalf("decrypted apiKey = %q", decoded["apiKey"])
	}
}

func TestMaskCredentialPayload(t *testing.T) {
	masked := MaskCredentialPayload(map[string]any{"apiKey": "sk-1234567890abcd"})
	if strings.Contains(masked, "1234567890") {
		t.Fatalf("mask leaked secret body: %q", masked)
	}
	if masked != "sk-****abcd" {
		t.Fatalf("masked = %q", masked)
	}
}

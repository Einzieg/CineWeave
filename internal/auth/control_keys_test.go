package auth

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
)

func TestParseControlKeyTokenUsesFixedPublicIDBoundary(t *testing.T) {
	publicBytes := []byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255}
	secretBytes := []byte{255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
		255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255}
	publicID := base64.RawURLEncoding.EncodeToString(publicBytes)
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	if !strings.Contains(publicID, "_") || !strings.Contains(secret, "_") {
		t.Fatalf("test token does not exercise URL-safe underscore: %q/%q", publicID, secret)
	}
	token := controlKeyTokenPrefix + "_" + publicID + "_" + secret
	gotPublicID, gotSecret, err := parseControlKeyToken(token)
	if err != nil {
		t.Fatalf("parse control key: %v", err)
	}
	if gotPublicID != publicID || gotSecret != secret {
		t.Fatalf("parsed token=%q/%q, want %q/%q", gotPublicID, gotSecret, publicID, secret)
	}
}

func TestParseControlKeyTokenRejectsMalformedValues(t *testing.T) {
	for _, token := range []string{"", "cwuk_v1_short_secret", "other_v1_public_secret", "cwuk_v1_****************_secret"} {
		if _, _, err := parseControlKeyToken(token); !errors.Is(err, ErrControlKeyInvalid) {
			t.Fatalf("parse %q error=%v, want invalid key", token, err)
		}
	}
}

func TestControlKeyMetadataRequiresRotationAfterCredentialChange(t *testing.T) {
	metadata := controlKeyMetadata(controlKeyRow{
		ID: "key", Name: controlKeyName, Prefix: "cwuk_v1_test", Status: "active",
		UserStatus: "active", CredentialVersion: 1, UserCredential: 2,
	})
	if metadata.Status != "requires_rotation" || !metadata.CanRotate || metadata.CanRevoke {
		t.Fatalf("metadata=%+v", metadata)
	}
}

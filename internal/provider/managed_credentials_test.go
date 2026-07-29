package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagedProviderAccountFingerprintIsCanonical(t *testing.T) {
	first, firstHash, err := normalizeManagedProviderAccountRequest(
		EnsureManagedProviderAccountRequest{
			OrganizationID:      " org ",
			CreatedByUserID:     " user ",
			ManagementReference: " billing:account ",
			Name:                " Wallet ",
			ConnectorKey:        " openai-compatible ",
			BaseURL:             "https://example.test/v1/",
			AuthType:            "",
			Config:              json.RawMessage(`{"z":1,"a":2}`),
		},
	)
	if err != nil {
		t.Fatalf("normalize first request: %v", err)
	}
	_, secondHash, err := normalizeManagedProviderAccountRequest(
		EnsureManagedProviderAccountRequest{
			OrganizationID:      "org",
			CreatedByUserID:     "user",
			ManagementReference: "billing:account",
			Name:                "Wallet",
			ConnectorKey:        "openai-compatible",
			BaseURL:             "https://example.test/v1",
			AuthType:            "bearer",
			Config:              json.RawMessage(`{"a":2,"z":1}`),
		},
	)
	if err != nil {
		t.Fatalf("normalize second request: %v", err)
	}
	if first.BaseURL != "https://example.test/v1" {
		t.Fatalf("normalized BaseURL = %q", first.BaseURL)
	}
	if firstHash != secondHash {
		t.Fatalf("canonical hashes differ: %s != %s", firstHash, secondHash)
	}
}

func TestManagedCredentialImportFingerprintIncludesSecret(t *testing.T) {
	base := ImportManagedCredentialRequest{
		AttemptID:            "attempt-1",
		OrganizationID:       "org-1",
		ProviderAccountID:    "account-1",
		CredentialKey:        "default",
		CredentialType:       "api_key",
		ImportIdempotencyKey: "import-1",
		RequestHash:          strings.Repeat("a", 64),
		ManagementReference:  "credential-1",
		Credential:           map[string]any{"apiKey": "secret-one"},
	}
	_, firstHash, firstSecretHash, err := normalizeManagedCredentialImportRequest(base)
	if err != nil {
		t.Fatalf("normalize first import: %v", err)
	}
	base.Credential = map[string]any{"apiKey": "secret-two"}
	_, secondHash, secondSecretHash, err := normalizeManagedCredentialImportRequest(base)
	if err != nil {
		t.Fatalf("normalize second import: %v", err)
	}
	if firstHash == secondHash || firstSecretHash == secondSecretHash {
		t.Fatal("credential import fingerprint did not bind the secret")
	}
	for _, value := range []string{firstHash, secondHash, firstSecretHash, secondSecretHash} {
		if !isSHA256Hex(value) {
			t.Fatalf("digest = %q, want lowercase SHA-256", value)
		}
	}
}

func TestGatewayClientManagedCredentialRoutes(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer service-token" {
			t.Fatalf("Authorization = %q", r.Header.Get("Authorization"))
		}
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/internal/provider/v1/managed-accounts/ensure":
			writeGatewayClientTestEnvelope(t, w, ManagedProviderAccountResult{
				ID:                  "account",
				OrganizationID:      "org",
				ManagementReference: "managed-account",
			})
		case "/internal/provider/v1/credential-imports",
			"/internal/provider/v1/credential-imports/resolve",
			"/internal/provider/v1/credential-imports/activate":
			writeGatewayClientTestEnvelope(t, w, ManagedCredentialResult{
				State:                       ManagedCredentialStateActive,
				ImportID:                    "import",
				ProviderCredentialID:        "credential",
				ProviderCredentialReference: "managed-credential",
			})
		case "/internal/provider/v1/credential-imports/revoke":
			writeGatewayClientTestEnvelope(t, w, map[string]bool{"revoked": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := &GatewayClient{
		BaseURL: server.URL,
		Token:   "service-token",
		Client:  server.Client(),
	}
	ctx := context.Background()
	if _, err := client.EnsureManagedProviderAccount(
		ctx,
		EnsureManagedProviderAccountRequest{},
	); err != nil {
		t.Fatalf("ensure managed account: %v", err)
	}
	if _, err := client.ImportManagedCredential(
		ctx,
		ImportManagedCredentialRequest{},
	); err != nil {
		t.Fatalf("import managed credential: %v", err)
	}
	if _, err := client.ResolveManagedCredential(
		ctx,
		ResolveManagedCredentialRequest{},
	); err != nil {
		t.Fatalf("resolve managed credential: %v", err)
	}
	if _, err := client.ActivateManagedCredential(
		ctx,
		ActivateManagedCredentialRequest{},
	); err != nil {
		t.Fatalf("activate managed credential: %v", err)
	}
	if err := client.RevokeManagedCredential(
		ctx,
		RevokeManagedCredentialRequest{},
	); err != nil {
		t.Fatalf("revoke managed credential: %v", err)
	}
	want := []string{
		"/internal/provider/v1/managed-accounts/ensure",
		"/internal/provider/v1/credential-imports",
		"/internal/provider/v1/credential-imports/resolve",
		"/internal/provider/v1/credential-imports/activate",
		"/internal/provider/v1/credential-imports/revoke",
	}
	if strings.Join(paths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("paths = %#v, want %#v", paths, want)
	}
}

func writeGatewayClientTestEnvelope(t *testing.T, w http.ResponseWriter, data any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
		t.Fatalf("encode test response: %v", err)
	}
}

func TestManagedCredentialValidationRejectsMalformedHashes(t *testing.T) {
	_, _, _, err := normalizeManagedCredentialImportRequest(
		ImportManagedCredentialRequest{
			AttemptID:            "attempt",
			OrganizationID:       "org",
			ProviderAccountID:    "account",
			CredentialKey:        "default",
			ImportIdempotencyKey: "import",
			RequestHash:          "not-a-hash",
			ManagementReference:  "credential",
			Credential:           map[string]any{"apiKey": "secret"},
		},
	)
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("error = %v, want ErrValidation", err)
	}
}

package edition

import (
	"errors"
	"testing"
	"time"
)

func TestDeploymentLicenseUsesMonotonicTrustedTime(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	input := validLicenseInput(now)
	input.SystemTime = now.Add(-2 * time.Hour)
	input.AllowedClockSkew = 5 * time.Minute
	input.PersistedState.LastTrustedTime = now

	evaluation, err := EvaluateDeploymentLicense(input)
	if err != nil {
		t.Fatalf("EvaluateDeploymentLicense: %v", err)
	}
	if evaluation.State != LicenseStateClockRollbackSuspected ||
		evaluation.EffectiveTime != now ||
		evaluation.OperationalState.RestrictionReason != RestrictionClockRollback {
		t.Fatalf("evaluation = %+v", evaluation)
	}
	assertLicenseOperationDenial(t, evaluation, LicenseOperationPaidProviderCreate, DenialDeploymentClockRollbackSuspected)
	if err := AuthorizeLicenseOperation(evaluation, LicenseOperationRequest{Operation: LicenseOperationReadOrExport}); err != nil {
		t.Fatalf("read must remain available: %v", err)
	}
}

func TestDeploymentLicenseRejectsStaleSerialAndRevocationGeneration(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	for name, mutate := range map[string]func(*DeploymentLicenseValidationInput){
		"serial": func(input *DeploymentLicenseValidationInput) {
			input.PersistedState.HighestSerialNumber = input.Claims.SerialNumber + 1
		},
		"revocation": func(input *DeploymentLicenseValidationInput) {
			input.PersistedState.HighestRevocationGeneration = input.Claims.RevocationGeneration + 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := validLicenseInput(now)
			mutate(&input)
			evaluation, err := EvaluateDeploymentLicense(input)
			if err != nil {
				t.Fatalf("EvaluateDeploymentLicense: %v", err)
			}
			if evaluation.State != LicenseStateRevoked ||
				evaluation.OperationalState.RestrictionReason != RestrictionLicenseRevoked {
				t.Fatalf("evaluation = %+v", evaluation)
			}
		})
	}
}

func TestLicenseOperationMatrixPreservesDataAndBlocksNewSpend(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	validInput := validLicenseInput(now)
	valid, err := EvaluateDeploymentLicense(validInput)
	if err != nil {
		t.Fatalf("valid license: %v", err)
	}
	for _, operation := range []LicenseOperation{
		LicenseOperationReadOrExport,
		LicenseOperationCore,
		LicenseOperationRenewal,
		LicenseOperationCommercialWrite,
		LicenseOperationPaidProviderCreate,
		LicenseOperationAsyncPollOrCancel,
		LicenseOperationFinalization,
	} {
		if err := AuthorizeLicenseOperation(valid, LicenseOperationRequest{Operation: operation}); err != nil {
			t.Fatalf("valid license rejected %s: %v", operation, err)
		}
	}

	expiredInput := validLicenseInput(now)
	expiredInput.Claims.ExpiresAt = now
	expired, err := EvaluateDeploymentLicense(expiredInput)
	if err != nil {
		t.Fatalf("expired license: %v", err)
	}
	for _, operation := range []LicenseOperation{
		LicenseOperationReadOrExport,
		LicenseOperationCore,
		LicenseOperationRenewal,
		LicenseOperationAsyncPollOrCancel,
		LicenseOperationFinalization,
	} {
		if err := AuthorizeLicenseOperation(expired, LicenseOperationRequest{Operation: operation}); err != nil {
			t.Fatalf("expired license rejected safe operation %s: %v", operation, err)
		}
	}
	assertLicenseOperationDenial(t, expired, LicenseOperationCommercialWrite, DenialDeploymentLicenseExpired)
	assertLicenseOperationDenial(t, expired, LicenseOperationPaidProviderCreate, DenialDeploymentLicenseExpired)
	assertLicenseOperationDenial(t, expired, LicenseOperationIdempotentRecovery, DenialDeploymentLicenseExpired)
	if err := AuthorizeLicenseOperation(expired, LicenseOperationRequest{
		Operation:                        LicenseOperationIdempotentRecovery,
		ProvesNoAdditionalProviderCharge: true,
	}); err != nil {
		t.Fatalf("charge-free idempotent recovery must remain available: %v", err)
	}
}

func TestSignedGraceDoesNotAllowNewSpendUnlessClaimed(t *testing.T) {
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	graceUntil := now.Add(time.Hour)
	input := validLicenseInput(now)
	input.Claims.ExpiresAt = now.Add(-time.Minute)
	input.Claims.GraceUntil = &graceUntil

	evaluation, err := EvaluateDeploymentLicense(input)
	if err != nil {
		t.Fatalf("EvaluateDeploymentLicense: %v", err)
	}
	if evaluation.State != LicenseStateSignatureGrace {
		t.Fatalf("state = %s", evaluation.State)
	}
	assertLicenseOperationDenial(t, evaluation, LicenseOperationPaidProviderCreate, DenialDeploymentLicenseExpired)

	input.Claims.AllowPaidProviderCreateInGrace = true
	evaluation, err = EvaluateDeploymentLicense(input)
	if err != nil {
		t.Fatalf("EvaluateDeploymentLicense with spend claim: %v", err)
	}
	if err := AuthorizeLicenseOperation(evaluation, LicenseOperationRequest{Operation: LicenseOperationPaidProviderCreate}); err != nil {
		t.Fatalf("signed grace spend claim was not honored: %v", err)
	}
}

func TestBillingAuthorityIsolationRequiresOneAuthorityOrganizationAndAccount(t *testing.T) {
	valid := BillingAuthorityIsolationFacts{
		ContextAuthorityRef:        "authority-1",
		AccountAuthorityRef:        "authority-1",
		CredentialAuthorityRef:     "authority-1",
		ContextOrganizationID:      "org-1",
		AccountOrganizationID:      "org-1",
		CredentialOrganizationID:   "org-1",
		ContextBillingAccountID:    "account-1",
		CredentialBillingAccountID: "account-1",
	}
	if err := EvaluateBillingAuthorityIsolation(valid); err != nil {
		t.Fatalf("valid isolation facts: %v", err)
	}

	crossAuthority := valid
	crossAuthority.CredentialAuthorityRef = "authority-2"
	var authorizationErr AuthorizationError
	if err := EvaluateBillingAuthorityIsolation(crossAuthority); !errors.As(err, &authorizationErr) ||
		authorizationErr.Code != DenialBillingAuthorityMismatch {
		t.Fatalf("cross-authority error = %v", err)
	}
}

func validLicenseInput(now time.Time) DeploymentLicenseValidationInput {
	return DeploymentLicenseValidationInput{
		Claims: DeploymentLicenseClaims{
			CustomerRefHash:      "customer-hash",
			DeploymentID:         "deployment-1",
			Edition:              EditionEnterprise,
			FeatureKeys:          []FeatureKey{FeatureBillingBalance},
			NotBefore:            now.Add(-time.Hour),
			ExpiresAt:            now.Add(time.Hour),
			IssuedAt:             now.Add(-2 * time.Hour),
			SerialNumber:         10,
			RevocationGeneration: 4,
			LicenseVersion:       1,
		},
		SignatureValid:       true,
		ExpectedDeploymentID: "deployment-1",
		ExpectedEdition:      EditionEnterprise,
		SystemTime:           now,
		AllowedClockSkew:     5 * time.Minute,
	}
}

func assertLicenseOperationDenial(t *testing.T, evaluation DeploymentLicenseEvaluation, operation LicenseOperation, want DenialCode) {
	t.Helper()
	err := AuthorizeLicenseOperation(evaluation, LicenseOperationRequest{Operation: operation})
	var authorizationErr AuthorizationError
	if !errors.As(err, &authorizationErr) || authorizationErr.Code != want {
		t.Fatalf("%s error = %v, want %s", operation, err, want)
	}
}

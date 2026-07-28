package edition

import (
	"fmt"
	"strings"
	"time"
)

type LicenseState string

const (
	LicenseStateValid                  LicenseState = "valid"
	LicenseStateSignatureGrace         LicenseState = "signature_grace"
	LicenseStateInvalid                LicenseState = "invalid"
	LicenseStateNotYetValid            LicenseState = "not_yet_valid"
	LicenseStateExpired                LicenseState = "expired"
	LicenseStateRevoked                LicenseState = "revoked"
	LicenseStateClockRollbackSuspected LicenseState = "clock_rollback_suspected"
	LicenseStateDeploymentMismatch     LicenseState = "deployment_mismatch"
)

type LicenseOperation string

const (
	LicenseOperationReadOrExport       LicenseOperation = "read_or_export"
	LicenseOperationCore               LicenseOperation = "core_operation"
	LicenseOperationRenewal            LicenseOperation = "license_renewal"
	LicenseOperationCommercialWrite    LicenseOperation = "commercial_write"
	LicenseOperationPaidProviderCreate LicenseOperation = "paid_provider_create"
	LicenseOperationAsyncPollOrCancel  LicenseOperation = "async_poll_or_cancel"
	LicenseOperationFinalization       LicenseOperation = "finalization"
	LicenseOperationIdempotentRecovery LicenseOperation = "idempotent_recovery"
)

type DeploymentLicenseClaims struct {
	CustomerRefHash                string       `json:"customerRefHash"`
	DeploymentID                   string       `json:"deploymentId"`
	Edition                        Edition      `json:"edition"`
	FeatureKeys                    []FeatureKey `json:"featureKeys"`
	NotBefore                      time.Time    `json:"notBefore"`
	ExpiresAt                      time.Time    `json:"expiresAt"`
	IssuedAt                       time.Time    `json:"issuedAt"`
	GraceUntil                     *time.Time   `json:"graceUntil,omitempty"`
	SerialNumber                   uint64       `json:"serialNumber"`
	RevocationGeneration           uint64       `json:"revocationGeneration"`
	LicenseVersion                 int          `json:"licenseVersion"`
	AllowPaidProviderCreateInGrace bool         `json:"allowPaidProviderCreateInGrace"`
}

type TrustedLicenseState struct {
	LastTrustedTime             time.Time `json:"lastTrustedTime"`
	HighestSerialNumber         uint64    `json:"highestSerialNumber"`
	HighestRevocationGeneration uint64    `json:"highestRevocationGeneration"`
}

type DeploymentLicenseValidationInput struct {
	Claims               DeploymentLicenseClaims
	SignatureValid       bool
	ExpectedDeploymentID string
	ExpectedEdition      Edition
	SystemTime           time.Time
	AllowedClockSkew     time.Duration
	PersistedState       TrustedLicenseState
}

type DeploymentLicenseEvaluation struct {
	State            LicenseState            `json:"state"`
	EffectiveTime    time.Time               `json:"effectiveTime"`
	OperationalState OperationalState        `json:"operationalState"`
	NextTrustedState TrustedLicenseState     `json:"nextTrustedState"`
	Claims           DeploymentLicenseClaims `json:"-"`
}

type LicenseOperationRequest struct {
	Operation                        LicenseOperation
	ProvesNoAdditionalProviderCharge bool
}

func EvaluateDeploymentLicense(input DeploymentLicenseValidationInput) (DeploymentLicenseEvaluation, error) {
	if input.SystemTime.IsZero() {
		return DeploymentLicenseEvaluation{}, fmt.Errorf("system time is required")
	}
	if input.AllowedClockSkew < 0 {
		return DeploymentLicenseEvaluation{}, fmt.Errorf("allowed clock skew cannot be negative")
	}
	effectiveTime := input.SystemTime.UTC()
	persisted := input.PersistedState
	persisted.LastTrustedTime = persisted.LastTrustedTime.UTC()
	if persisted.LastTrustedTime.After(effectiveTime) {
		effectiveTime = persisted.LastTrustedTime
	}
	evaluation := DeploymentLicenseEvaluation{
		EffectiveTime:    effectiveTime,
		NextTrustedState: persisted,
		Claims:           cloneLicenseClaims(input.Claims),
	}
	if !persisted.LastTrustedTime.IsZero() &&
		input.SystemTime.UTC().Add(input.AllowedClockSkew).Before(persisted.LastTrustedTime) {
		return restrictLicense(evaluation, LicenseStateClockRollbackSuspected, RestrictionClockRollback), nil
	}
	if !input.SignatureValid {
		return restrictLicense(evaluation, LicenseStateInvalid, RestrictionLicenseInvalid), nil
	}
	if strings.TrimSpace(input.ExpectedDeploymentID) == "" ||
		input.Claims.DeploymentID != input.ExpectedDeploymentID ||
		input.Claims.Edition != input.ExpectedEdition ||
		(input.ExpectedEdition != EditionCloud && input.ExpectedEdition != EditionEnterprise) {
		return restrictLicense(evaluation, LicenseStateDeploymentMismatch, RestrictionDeploymentMismatch), nil
	}
	if input.Claims.SerialNumber < persisted.HighestSerialNumber ||
		input.Claims.RevocationGeneration < persisted.HighestRevocationGeneration {
		return restrictLicense(evaluation, LicenseStateRevoked, RestrictionLicenseRevoked), nil
	}
	if err := validateLicenseClaims(input.Claims); err != nil {
		return DeploymentLicenseEvaluation{}, err
	}

	evaluation.NextTrustedState.LastTrustedTime = effectiveTime
	if input.Claims.SerialNumber > evaluation.NextTrustedState.HighestSerialNumber {
		evaluation.NextTrustedState.HighestSerialNumber = input.Claims.SerialNumber
	}
	if input.Claims.RevocationGeneration > evaluation.NextTrustedState.HighestRevocationGeneration {
		evaluation.NextTrustedState.HighestRevocationGeneration = input.Claims.RevocationGeneration
	}
	if effectiveTime.Before(input.Claims.NotBefore.UTC()) {
		return restrictLicense(evaluation, LicenseStateNotYetValid, RestrictionLicenseNotYetValid), nil
	}
	if !effectiveTime.Before(input.Claims.ExpiresAt.UTC()) {
		if input.Claims.GraceUntil != nil && effectiveTime.Before(input.Claims.GraceUntil.UTC()) {
			evaluation.State = LicenseStateSignatureGrace
			evaluation.OperationalState = OperationalState{
				Mode:              OperationalModeCommercialRestricted,
				RestrictionReason: RestrictionLicenseExpired,
			}
			return evaluation, nil
		}
		return restrictLicense(evaluation, LicenseStateExpired, RestrictionLicenseExpired), nil
	}
	evaluation.State = LicenseStateValid
	evaluation.OperationalState = OperationalState{Mode: OperationalModeNormal}
	return evaluation, nil
}

func AuthorizeLicenseOperation(evaluation DeploymentLicenseEvaluation, request LicenseOperationRequest) error {
	switch request.Operation {
	case LicenseOperationReadOrExport, LicenseOperationCore, LicenseOperationRenewal,
		LicenseOperationAsyncPollOrCancel, LicenseOperationFinalization:
		return nil
	case LicenseOperationIdempotentRecovery:
		if request.ProvesNoAdditionalProviderCharge {
			return nil
		}
	case LicenseOperationCommercialWrite:
		if evaluation.State == LicenseStateValid {
			return nil
		}
	case LicenseOperationPaidProviderCreate:
		if evaluation.State == LicenseStateValid ||
			(evaluation.State == LicenseStateSignatureGrace && evaluation.Claims.AllowPaidProviderCreateInGrace) {
			return nil
		}
	default:
		return fmt.Errorf("license operation %q is invalid", request.Operation)
	}
	code := licenseStateDenialCode(evaluation.State)
	return newAuthorizationError(code, "deployment license does not allow this operation")
}

func EvaluateBillingAuthorityIsolation(facts BillingAuthorityIsolationFacts) error {
	values := []string{
		facts.ContextAuthorityRef,
		facts.AccountAuthorityRef,
		facts.CredentialAuthorityRef,
		facts.ContextOrganizationID,
		facts.AccountOrganizationID,
		facts.CredentialOrganizationID,
		facts.ContextBillingAccountID,
		facts.CredentialBillingAccountID,
	}
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return newAuthorizationError(DenialBillingAuthorityMismatch, "billing authority isolation facts are incomplete")
		}
	}
	if facts.ContextAuthorityRef != facts.AccountAuthorityRef ||
		facts.ContextAuthorityRef != facts.CredentialAuthorityRef ||
		facts.ContextOrganizationID != facts.AccountOrganizationID ||
		facts.ContextOrganizationID != facts.CredentialOrganizationID ||
		facts.ContextBillingAccountID != facts.CredentialBillingAccountID {
		return newAuthorizationError(DenialBillingAuthorityMismatch, "billing context, account, and credential must share one authority and organization")
	}
	return nil
}

func validateLicenseClaims(claims DeploymentLicenseClaims) error {
	if strings.TrimSpace(claims.CustomerRefHash) == "" ||
		strings.TrimSpace(claims.DeploymentID) == "" ||
		claims.LicenseVersion <= 0 ||
		claims.SerialNumber == 0 ||
		claims.IssuedAt.IsZero() ||
		claims.NotBefore.IsZero() ||
		claims.ExpiresAt.IsZero() {
		return fmt.Errorf("deployment license claims are incomplete")
	}
	if claims.Edition != EditionCloud && claims.Edition != EditionEnterprise {
		return fmt.Errorf("deployment license edition %q is invalid", claims.Edition)
	}
	if !claims.IssuedAt.UTC().After(claims.ExpiresAt.UTC()) &&
		claims.NotBefore.UTC().Before(claims.ExpiresAt.UTC()) {
		if claims.GraceUntil == nil || claims.GraceUntil.UTC().After(claims.ExpiresAt.UTC()) {
			return nil
		}
	}
	return fmt.Errorf("deployment license time bounds are invalid")
}

func restrictLicense(evaluation DeploymentLicenseEvaluation, state LicenseState, reason RestrictionReason) DeploymentLicenseEvaluation {
	evaluation.State = state
	evaluation.OperationalState = OperationalState{
		Mode:              OperationalModeCommercialRestricted,
		RestrictionReason: reason,
	}
	return evaluation
}

func licenseStateDenialCode(state LicenseState) DenialCode {
	switch state {
	case LicenseStateNotYetValid:
		return DenialDeploymentLicenseNotYetValid
	case LicenseStateExpired, LicenseStateSignatureGrace:
		return DenialDeploymentLicenseExpired
	case LicenseStateRevoked:
		return DenialDeploymentLicenseRevoked
	case LicenseStateClockRollbackSuspected:
		return DenialDeploymentClockRollbackSuspected
	default:
		return DenialDeploymentLicenseInvalid
	}
}

func cloneLicenseClaims(claims DeploymentLicenseClaims) DeploymentLicenseClaims {
	claims.FeatureKeys = append([]FeatureKey(nil), claims.FeatureKeys...)
	if claims.GraceUntil != nil {
		value := claims.GraceUntil.UTC()
		claims.GraceUntil = &value
	}
	return claims
}

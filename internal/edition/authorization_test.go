package edition

import (
	"errors"
	"testing"
)

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

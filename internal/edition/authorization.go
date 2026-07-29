package edition

import "strings"

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
			return newAuthorizationError(
				DenialBillingAuthorityMismatch,
				"billing authority isolation facts are incomplete",
			)
		}
	}
	if facts.ContextAuthorityRef != facts.AccountAuthorityRef ||
		facts.ContextAuthorityRef != facts.CredentialAuthorityRef ||
		facts.ContextOrganizationID != facts.AccountOrganizationID ||
		facts.ContextOrganizationID != facts.CredentialOrganizationID ||
		facts.ContextBillingAccountID != facts.CredentialBillingAccountID {
		return newAuthorizationError(
			DenialBillingAuthorityMismatch,
			"billing context, account, and credential must share one authority and organization",
		)
	}
	return nil
}

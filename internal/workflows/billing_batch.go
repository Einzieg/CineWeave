package workflows

import (
	"strings"

	editionpkg "github.com/Einzieg/cineweave/internal/edition"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	batchBillingBalanceStopVersion = "batch-billing-balance-stop-v1"
	billingInsufficientBalanceCode = string(editionpkg.DenialBillingInsufficientBalance)
	billingInsufficientBalanceText = "New API 账户余额不足，未发起该批量条目"
)

func batchStopsOnInsufficientBalance(ctx workflow.Context) bool {
	return workflow.GetVersion(
		ctx,
		batchBillingBalanceStopVersion,
		workflow.DefaultVersion,
		1,
	) != workflow.DefaultVersion
}

func isBillingInsufficientBalanceCode(code string) bool {
	normalized := strings.ToLower(strings.TrimSpace(code))
	return normalized == billingInsufficientBalanceCode ||
		normalized == "billing_insufficient_balance"
}

func billingInsufficientBalanceFailure(err error) (code string, message string, ok bool) {
	if err == nil {
		return "", "", false
	}
	code, message = workflowExecutionError(err)
	if !isBillingInsufficientBalanceCode(code) {
		return "", "", false
	}
	if strings.TrimSpace(message) == "" {
		message = billingInsufficientBalanceText
	}
	return code, message, true
}

func billingInsufficientBalanceError(code, message string) error {
	code = strings.TrimSpace(code)
	if !isBillingInsufficientBalanceCode(code) {
		code = billingInsufficientBalanceCode
	}
	message = strings.TrimSpace(message)
	if message == "" {
		message = billingInsufficientBalanceText
	}
	return temporal.NewNonRetryableApplicationError(message, code, nil)
}

func unstartedBillingInsufficientBalanceFailure(code, message string) (string, string) {
	code = strings.TrimSpace(code)
	if !isBillingInsufficientBalanceCode(code) {
		code = billingInsufficientBalanceCode
	}
	message = billingInsufficientBalanceText
	return code, message
}

func batchShotBillingInsufficientBalance(
	output BatchShotProductionOutput,
) (code string, message string, ok bool) {
	orderedShotIDs := append([]string(nil), output.FailedShotIDs...)
	orderedShotIDs = append(orderedShotIDs, output.TargetShotIDs...)
	seen := make(map[string]bool, len(orderedShotIDs))
	for _, shotID := range orderedShotIDs {
		if seen[shotID] {
			continue
		}
		seen[shotID] = true
		itemCode := output.ErrorCodes[shotID]
		if !isBillingInsufficientBalanceCode(itemCode) {
			continue
		}
		itemMessage := output.Errors[shotID]
		if strings.TrimSpace(itemMessage) == "" {
			itemMessage = billingInsufficientBalanceText
		}
		return itemCode, itemMessage, true
	}
	return "", "", false
}

func markBatchShotTargetsUnstartedForBalance(
	output *BatchShotProductionOutput,
	shotIDs []string,
	code string,
	message string,
) {
	if output == nil {
		return
	}
	code, message = unstartedBillingInsufficientBalanceFailure(code, message)
	if output.Errors == nil {
		output.Errors = map[string]string{}
	}
	if output.ErrorCodes == nil {
		output.ErrorCodes = map[string]string{}
	}
	for _, shotID := range shotIDs {
		if strings.TrimSpace(shotID) == "" {
			continue
		}
		if containsWorkflowString(output.SucceededShotIDs, shotID) ||
			containsWorkflowString(output.FailedShotIDs, shotID) ||
			containsWorkflowString(output.CancelledShotIDs, shotID) {
			continue
		}
		output.FailedShotIDs = append(output.FailedShotIDs, shotID)
		output.ErrorCodes[shotID] = code
		output.Errors[shotID] = message
	}
}

func containsWorkflowString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

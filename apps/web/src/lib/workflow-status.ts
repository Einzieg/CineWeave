const terminalWorkflowStatuses = new Set([
  "succeeded",
  "partial_succeeded",
  "completed",
  "failed",
  "cancelled",
  "skipped",
]);

export function isTerminalWorkflowStatus(status: string) {
  return terminalWorkflowStatuses.has(status.trim().toLowerCase());
}

export function isActiveWorkflowStatus(status: string) {
  return !isTerminalWorkflowStatus(status);
}

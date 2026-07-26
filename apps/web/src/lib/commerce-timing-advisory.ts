export type CommerceTimingAdvisory = {
  estimatedSeconds: number;
  targetSeconds: number;
  deltaSeconds: number;
  overTarget: boolean;
};

export function commerceTimingAdvisory(
  estimatedSeconds: number | undefined,
  targetSeconds: number,
): CommerceTimingAdvisory | null {
  if (
    typeof estimatedSeconds !== "number"
    || !Number.isFinite(estimatedSeconds)
    || estimatedSeconds < 0
    || !Number.isFinite(targetSeconds)
    || targetSeconds <= 0
  ) {
    return null;
  }
  const deltaSeconds = Math.abs(estimatedSeconds - targetSeconds);
  return {
    estimatedSeconds,
    targetSeconds,
    deltaSeconds,
    overTarget: estimatedSeconds > targetSeconds,
  };
}

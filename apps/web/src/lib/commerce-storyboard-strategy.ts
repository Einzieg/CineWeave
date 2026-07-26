import type { CommerceStoryboardStrategy } from "./types";

export type CommerceStoryboardStrategyAction = "preview" | "rebuild";

export function commerceStoryboardStrategyAction(
  currentStrategy: CommerceStoryboardStrategy | undefined,
  selectedStrategy: CommerceStoryboardStrategy,
): CommerceStoryboardStrategyAction {
  return currentStrategy === selectedStrategy ? "preview" : "rebuild";
}

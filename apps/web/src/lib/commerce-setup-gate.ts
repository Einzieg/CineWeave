import type { CommerceSetupState } from "./types";

export type CommerceSetupPreparationGate = {
  blocked: boolean;
  actionLabel: string;
};

export function commerceSetupPreparationGate(
  setupSessionId: string,
  state?: CommerceSetupState,
): CommerceSetupPreparationGate {
  if (!setupSessionId || state === "completed") {
    return { blocked: false, actionLabel: "准备并生成分镜" };
  }
  if (!state) {
    return { blocked: true, actionLabel: "正在读取项目准备状态" };
  }
  if (state === "waiting_user_confirmation") {
    return { blocked: true, actionLabel: "正在自动识别视频语言" };
  }
  if (state === "failed") {
    return { blocked: true, actionLabel: "先重试项目准备" };
  }
  if (state === "needs_user_review") {
    return { blocked: true, actionLabel: "先处理项目准备问题" };
  }
  if (state === "abandoned") {
    return { blocked: true, actionLabel: "项目创建已放弃" };
  }
  return { blocked: true, actionLabel: "等待项目准备完成" };
}

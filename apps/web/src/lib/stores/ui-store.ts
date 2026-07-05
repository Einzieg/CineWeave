"use client";

import { create } from "zustand";

type UiState = {
  /** 剧本助手抽屉 */
  assistantOpen: boolean;
  assistantSessionId: string;
  /** 唤起助手时携带的页面上下文(如“改写第 3 场”) */
  assistantContext: string;
  /** 任务活动抽屉 */
  activityOpen: boolean;
  setAssistantOpen: (open: boolean) => void;
  setAssistantSessionId: (sessionId: string) => void;
  openAssistant: (context?: string) => void;
  clearAssistantContext: () => void;
  setActivityOpen: (open: boolean) => void;
};

export const useUiStore = create<UiState>((set) => ({
  assistantOpen: false,
  assistantSessionId: "",
  assistantContext: "",
  activityOpen: false,
  setAssistantOpen: (open) => set({ assistantOpen: open }),
  setAssistantSessionId: (sessionId) => set({ assistantSessionId: sessionId }),
  openAssistant: (context = "") => set({ assistantOpen: true, assistantContext: context }),
  clearAssistantContext: () => set({ assistantContext: "" }),
  setActivityOpen: (open) => set({ activityOpen: open }),
}));

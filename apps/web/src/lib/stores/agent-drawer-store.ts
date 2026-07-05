import { create } from "zustand";

interface AgentDrawerStore {
  isOpen: boolean;
  currentSessionId: string | null;
  agentType: string | null;
  context: Record<string, unknown>;

  open: (sessionId?: string, agentType?: string) => void;
  close: () => void;
  setSessionId: (sessionId: string | null) => void;
  setAgentType: (agentType: string | null) => void;
  setContext: (context: Record<string, unknown>) => void;
}

export const useAgentDrawerStore = create<AgentDrawerStore>((set) => ({
  isOpen: false,
  currentSessionId: null,
  agentType: null,
  context: {},

  open: (sessionId, agentType) => set({
    isOpen: true,
    currentSessionId: sessionId || null,
    agentType: agentType || null
  }),

  close: () => set({ isOpen: false }),

  setSessionId: (sessionId) => set({ currentSessionId: sessionId }),

  setAgentType: (agentType) => set({ agentType }),

  setContext: (context) => set({ context }),
}));

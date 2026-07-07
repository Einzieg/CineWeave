"use client";

import {
  useMutation,
  useQuery,
  useQueryClient,
  type QueryKey,
  type UseMutationOptions,
  type UseQueryOptions,
} from "@tanstack/react-query";
import { useCallback } from "react";
import { StudioApiError, studioApi } from "@/lib/api-client";
import { sessionFromAuthResponse, useStudioSession } from "@/lib/session";
import type { StudioSession } from "@/lib/types";

export function orgScopedKey(organizationId: string, key: QueryKey): QueryKey {
  return ["org", organizationId, ...key];
}

let refreshInFlight: { refreshToken: string; promise: Promise<StudioSession> } | null = null;
let lastRefreshResult: { refreshToken: string; session: StudioSession; refreshedAt: number } | null = null;

type ApiQueryOptions<TData> = Omit<UseQueryOptions<TData, Error, TData, QueryKey>, "queryKey" | "queryFn" | "enabled"> & {
  key: QueryKey;
  queryFn: (session: StudioSession) => Promise<TData>;
  enabled?: boolean;
};

/** 会话感知的 useQuery 封装:自动追加组织前缀、注入 session、等待会话就绪。 */
export function useApiQuery<TData>({ key, queryFn, enabled = true, ...rest }: ApiQueryOptions<TData>) {
  const { session, hydrated, ready, setSession, clearSession } = useStudioSession();
  return useQuery({
    ...rest,
    queryKey: orgScopedKey(session.organizationId, key),
    queryFn: () => withFreshSession(session, setSession, clearSession, queryFn),
    enabled: hydrated && ready && enabled,
  });
}

type ApiMutationOptions<TData, TVariables> = Omit<UseMutationOptions<TData, Error, TVariables>, "mutationFn"> & {
  mutationFn: (session: StudioSession, variables: TVariables) => Promise<TData>;
};

/** 会话感知的 useMutation 封装。 */
export function useApiMutation<TData, TVariables = void>({ mutationFn, ...rest }: ApiMutationOptions<TData, TVariables>) {
  const { session, setSession, clearSession } = useStudioSession();
  return useMutation({
    ...rest,
    mutationFn: (variables: TVariables) =>
      withFreshSession(session, setSession, clearSession, (freshSession) => mutationFn(freshSession, variables)),
  });
}

/** 返回按组织前缀批量失效 query 的函数。 */
export function useInvalidateKeys() {
  const queryClient = useQueryClient();
  const { session } = useStudioSession();
  const organizationId = session.organizationId;
  return useCallback(
    (keys: QueryKey[]) => {
      for (const key of keys) {
        void queryClient.invalidateQueries({ queryKey: orgScopedKey(organizationId, key) });
      }
    },
    [organizationId, queryClient],
  );
}

async function withFreshSession<TData>(
  session: StudioSession,
  setSession: (next: StudioSession) => void,
  clearSession: () => void,
  run: (session: StudioSession) => Promise<TData>,
): Promise<TData> {
  if (!session.accessToken.trim() || !session.organizationId.trim()) {
    throw new StudioApiError("登录已过期，请重新登录", "UNAUTHENTICATED", 401, false);
  }

  try {
    return await run(session);
  } catch (error) {
    if (!(error instanceof StudioApiError) || error.status !== 401 || !session.refreshToken.trim()) {
      throw error;
    }

    let nextSession: StudioSession;
    try {
      nextSession = await refreshSessionOnce(session);
    } catch {
      clearSession();
      throw new StudioApiError("登录已过期，请重新登录", "UNAUTHENTICATED", 401, false);
    }
    setSession(nextSession);
    return run(nextSession);
  }
}

async function refreshSessionOnce(session: StudioSession): Promise<StudioSession> {
  const refreshToken = session.refreshToken.trim();
  if (!refreshToken) {
    throw new StudioApiError("登录已过期，请重新登录", "UNAUTHENTICATED", 401, false);
  }

  if (lastRefreshResult?.refreshToken === refreshToken && Date.now() - lastRefreshResult.refreshedAt < 30_000) {
    return lastRefreshResult.session;
  }

  if (!refreshInFlight || refreshInFlight.refreshToken !== refreshToken) {
    refreshInFlight = {
      refreshToken,
      promise: studioApi.refreshAuth(refreshToken).then((response) => sessionFromAuthResponse(response, session.currentProjectId)),
    };
  }

  try {
    const nextSession = await refreshInFlight.promise;
    lastRefreshResult = { refreshToken, session: nextSession, refreshedAt: Date.now() };
    return nextSession;
  } finally {
    if (refreshInFlight?.refreshToken === refreshToken) {
      refreshInFlight = null;
    }
  }
}

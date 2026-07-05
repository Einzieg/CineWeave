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
import { useStudioSession } from "@/lib/session";
import type { StudioSession } from "@/lib/types";

export function orgScopedKey(organizationId: string, key: QueryKey): QueryKey {
  return ["org", organizationId, ...key];
}

type ApiQueryOptions<TData> = Omit<UseQueryOptions<TData, Error, TData, QueryKey>, "queryKey" | "queryFn" | "enabled"> & {
  key: QueryKey;
  queryFn: (session: StudioSession) => Promise<TData>;
  enabled?: boolean;
};

/** 会话感知的 useQuery 封装:自动追加组织前缀、注入 session、等待会话就绪。 */
export function useApiQuery<TData>({ key, queryFn, enabled = true, ...rest }: ApiQueryOptions<TData>) {
  const { session, hydrated, ready } = useStudioSession();
  return useQuery({
    ...rest,
    queryKey: orgScopedKey(session.organizationId, key),
    queryFn: () => queryFn(session),
    enabled: hydrated && ready && enabled,
  });
}

type ApiMutationOptions<TData, TVariables> = Omit<UseMutationOptions<TData, Error, TVariables>, "mutationFn"> & {
  mutationFn: (session: StudioSession, variables: TVariables) => Promise<TData>;
};

/** 会话感知的 useMutation 封装。 */
export function useApiMutation<TData, TVariables = void>({ mutationFn, ...rest }: ApiMutationOptions<TData, TVariables>) {
  const { session } = useStudioSession();
  return useMutation({
    ...rest,
    mutationFn: (variables: TVariables) => mutationFn(session, variables),
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

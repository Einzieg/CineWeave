import { QueryClient } from "@tanstack/react-query";
import { StudioApiError } from "@/lib/api-client";

export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        refetchOnWindowFocus: true,
        retry: (failureCount, error) => {
          if (error instanceof StudioApiError && !error.retryable && error.status >= 400 && error.status < 500) {
            return false;
          }
          return failureCount < 2;
        },
      },
    },
  });
}

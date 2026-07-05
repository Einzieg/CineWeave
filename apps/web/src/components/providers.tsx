"use client";

import { QueryClientProvider } from "@tanstack/react-query";
import { ThemeProvider } from "next-themes";
import { useState } from "react";
import type { ReactNode } from "react";
import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import { createQueryClient } from "@/lib/query/query-client";
import { StudioSessionProvider } from "@/lib/session";

export function Providers({ children }: { children: ReactNode }) {
  const [queryClient] = useState(createQueryClient);
  return (
    <ThemeProvider attribute="class" defaultTheme="dark" enableSystem={false} disableTransitionOnChange>
      <QueryClientProvider client={queryClient}>
        <StudioSessionProvider>
          <TooltipProvider delayDuration={200}>{children}</TooltipProvider>
          <Toaster richColors closeButton position="top-right" />
        </StudioSessionProvider>
      </QueryClientProvider>
    </ThemeProvider>
  );
}

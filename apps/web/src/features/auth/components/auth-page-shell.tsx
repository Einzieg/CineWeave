"use client";

import type { ReactNode } from "react";
import { Card, CardContent } from "@/components/ui/card";

export function AuthPageShell({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <main className="grid min-h-svh bg-background px-4 py-10">
      <div className="mx-auto flex w-full max-w-md flex-col justify-center">
        <div className="mb-8">
          <div className="mb-4 grid h-11 w-11 place-items-center rounded-lg bg-primary text-base font-bold text-primary-foreground">
            影
          </div>
          <p className="text-sm font-medium text-primary">影织</p>
          <h1 className="mt-2 text-2xl font-semibold tracking-tight">{title}</h1>
          <p className="mt-2 text-sm leading-6 text-muted-foreground">{description}</p>
        </div>
        <Card>
          <CardContent className="pt-6">{children}</CardContent>
        </Card>
      </div>
    </main>
  );
}

"use client";

import type { FormEvent, ReactNode } from "react";

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
    <main className="grid min-h-svh bg-slate-50 px-4 py-10 text-slate-950">
      <div className="mx-auto flex w-full max-w-md flex-col justify-center">
        <div className="mb-8">
          <div className="mb-4 grid h-11 w-11 place-items-center rounded-lg bg-blue-600 text-base font-semibold text-white">影</div>
          <p className="text-sm font-medium text-blue-700">影织</p>
          <h1 className="mt-2 text-2xl font-semibold tracking-normal text-slate-950">{title}</h1>
          <p className="mt-2 text-sm leading-6 text-slate-500">{description}</p>
        </div>
        <div className="rounded-lg border border-slate-200 bg-white p-5 shadow-sm shadow-slate-200/70">{children}</div>
      </div>
    </main>
  );
}

export function AuthForm({
  children,
  onSubmit,
}: {
  children: ReactNode;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void;
}) {
  return (
    <form className="grid gap-4" onSubmit={onSubmit}>
      {children}
    </form>
  );
}

export function AuthField({
  label,
  value,
  onChange,
  type = "text",
  autoComplete,
  required = true,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  type?: string;
  autoComplete?: string;
  required?: boolean;
}) {
  return (
    <label className="grid gap-1.5 text-sm">
      <span className="font-medium text-slate-700">{label}</span>
      <input
        autoComplete={autoComplete}
        className="studio-input"
        onChange={(event) => onChange(event.target.value)}
        required={required}
        type={type}
        value={value}
      />
    </label>
  );
}

export function AuthError({ message }: { message: string }) {
  if (!message) {
    return null;
  }
  return <p className="rounded-md border border-rose-200 bg-rose-50 px-3 py-2 text-sm text-rose-700">{message}</p>;
}

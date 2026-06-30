import { CineWeaveConsole } from "@/components/cineweave-console";
import type { Route } from "next";
import Link from "next/link";

export default function Page() {
  return (
    <main className="min-h-screen bg-zinc-950 text-zinc-100">
      <div className="border-b border-white/10 bg-zinc-950 px-5 py-4">
        <div className="mx-auto flex max-w-7xl items-center justify-between gap-3">
          <div>
            <h1 className="text-xl font-semibold">旧版演示控制台</h1>
            <p className="mt-1 text-sm text-zinc-500">保留旧 Demo Console 便于回归，但不作为默认入口。</p>
          </div>
          <Link className="studio-button" href={"/dashboard" as Route}>
            返回总览
          </Link>
        </div>
      </div>
      <CineWeaveConsole />
    </main>
  );
}

import { FileText } from "lucide-react";
import type { ReactNode } from "react";

export function EmptyState({ title, description, action }: { title: string; description: string; action?: ReactNode }) {
  return (
    <div className="grid min-h-48 place-items-center rounded-lg border border-dashed border-slate-200 bg-slate-50 px-6 py-10 text-center">
      <div className="max-w-md">
        <div className="mx-auto grid h-10 w-10 place-items-center rounded-lg bg-slate-100 text-blue-700">
          <FileText size={18} />
        </div>
        <h3 className="mt-4 text-base font-semibold text-slate-900">{title}</h3>
        <p className="mt-2 text-sm leading-6 text-slate-600">{description}</p>
        {action ? <div className="mt-5 flex justify-center">{action}</div> : null}
      </div>
    </div>
  );
}

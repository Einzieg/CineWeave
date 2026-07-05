import { FileText } from "lucide-react";
import type { ReactNode } from "react";

export function EmptyState({ title, description, action }: { title: string; description: string; action?: ReactNode }) {
  return (
    <div className="grid min-h-48 place-items-center rounded-lg border border-dashed bg-muted/30 px-6 py-10 text-center">
      <div className="max-w-md">
        <div className="mx-auto grid h-10 w-10 place-items-center rounded-lg bg-muted text-muted-foreground">
          <FileText className="h-5 w-5" />
        </div>
        <h3 className="mt-4 text-base font-semibold">{title}</h3>
        <p className="mt-2 text-sm leading-6 text-muted-foreground">{description}</p>
        {action ? <div className="mt-5 flex justify-center">{action}</div> : null}
      </div>
    </div>
  );
}

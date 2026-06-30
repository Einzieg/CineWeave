import { AlertTriangle } from "lucide-react";

export function ErrorPanel({ message }: { message?: string }) {
  if (!message) {
    return null;
  }
  return (
    <div className="flex items-start gap-3 rounded-lg border border-rose-300/20 bg-rose-500/10 px-4 py-3 text-sm text-rose-100">
      <AlertTriangle className="mt-0.5 shrink-0" size={16} />
      <p>{message}</p>
    </div>
  );
}

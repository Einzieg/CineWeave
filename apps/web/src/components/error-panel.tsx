import { AlertTriangle } from "lucide-react";

export function ErrorPanel({ message }: { message?: string }) {
  if (!message) {
    return null;
  }
  return (
    <div className="flex items-start gap-3 rounded-lg border border-rose-200 bg-rose-50 px-4 py-3 text-sm text-rose-700">
      <AlertTriangle className="mt-0.5 shrink-0" size={16} />
      <p>{message}</p>
    </div>
  );
}

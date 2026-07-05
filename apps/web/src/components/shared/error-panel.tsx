import { AlertTriangle } from "lucide-react";

export function ErrorPanel({ message }: { message?: string }) {
  if (!message) {
    return null;
  }
  return (
    <div className="flex items-start gap-3 rounded-lg border border-destructive/50 bg-destructive/10 px-4 py-3 text-sm text-destructive">
      <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
      <p>{message}</p>
    </div>
  );
}

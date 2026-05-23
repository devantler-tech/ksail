import { CircleAlert, Server } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "./ui.tsx";

export function EmptyState({
  title,
  description,
  action,
}: {
  title: string;
  description: string;
  action?: ReactNode;
}) {
  return (
    <div className="flex flex-col items-center justify-center rounded-xl border border-dashed border-slate-300 bg-white px-6 py-16 text-center dark:border-slate-700 dark:bg-slate-900">
      <span className="flex size-12 items-center justify-center rounded-full bg-slate-100 text-slate-400 dark:bg-slate-800">
        <Server className="size-6" aria-hidden />
      </span>
      <h3 className="mt-4 text-sm font-semibold text-slate-900 dark:text-white">{title}</h3>
      <p className="mt-1 max-w-sm text-sm text-slate-500 dark:text-slate-400">{description}</p>
      {action ? <div className="mt-5">{action}</div> : null}
    </div>
  );
}

export function ErrorBanner({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div className="flex items-start gap-3 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-800 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300">
      <CircleAlert className="mt-0.5 size-5 shrink-0 text-red-500" aria-hidden />
      <p className="flex-1 break-words">{message}</p>
      {onRetry ? (
        <Button variant="secondary" size="sm" onClick={onRetry}>
          Retry
        </Button>
      ) : null}
    </div>
  );
}

export function TableSkeleton() {
  return (
    <div className="overflow-hidden rounded-xl border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900">
      <div className="h-11 border-b border-slate-200 bg-slate-50 dark:border-slate-800 dark:bg-slate-800/50" />
      <div className="divide-y divide-slate-100 dark:divide-slate-800">
        {[0, 1, 2, 3].map((row) => (
          <div key={row} className="flex items-center gap-4 px-4 py-4">
            <div className="h-4 w-32 animate-pulse rounded bg-slate-200 dark:bg-slate-700" />
            <div className="h-4 w-20 animate-pulse rounded bg-slate-200 dark:bg-slate-700" />
            <div className="h-5 w-24 animate-pulse rounded-full bg-slate-200 dark:bg-slate-700" />
            <div className="ml-auto h-4 w-16 animate-pulse rounded bg-slate-200 dark:bg-slate-700" />
          </div>
        ))}
      </div>
    </div>
  );
}

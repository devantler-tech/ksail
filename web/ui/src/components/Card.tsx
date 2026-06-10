import type { ReactNode } from "react";
import { cx } from "../lib/cx.ts";

// Card is the dashboard panel chrome shared by the Overview's cards (live health, spec/status,
// resource usage): a bordered surface with an uppercase title row.
export function Card({
  title,
  icon,
  children,
  className,
}: {
  title: string;
  icon: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cx(
        "rounded-xl border border-slate-200 bg-white p-4 shadow-sm dark:border-slate-800 dark:bg-slate-900",
        className,
      )}
    >
      <div className="mb-3 flex items-center gap-2 text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400">
        {icon}
        {title}
      </div>
      {children}
    </div>
  );
}

// Field renders a label/value row in the Spec and Status cards.
export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-baseline justify-between gap-3 py-1.5">
      <dt className="shrink-0 text-xs text-slate-500 dark:text-slate-400">{label}</dt>
      <dd className="min-w-0 truncate text-right text-sm text-slate-700 dark:text-slate-200">{children}</dd>
    </div>
  );
}

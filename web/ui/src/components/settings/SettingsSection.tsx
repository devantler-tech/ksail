import type { ReactNode } from "react";

// SettingsSection is the shared chrome for a settings category: a heading + optional description,
// a bordered card holding the controls, and an optional right-aligned footer (e.g. a Save button).
// Consolidates the section markup the categories would otherwise each repeat.
export function SettingsSection({
  title,
  description,
  children,
  footer,
}: {
  title: string;
  description?: ReactNode;
  children: ReactNode;
  footer?: ReactNode;
}) {
  return (
    <section className="space-y-4">
      <div>
        <h2 className="text-base font-semibold text-slate-900 dark:text-white">{title}</h2>
        {description ? (
          <p className="mt-1 text-sm text-slate-500 dark:text-slate-400">{description}</p>
        ) : null}
      </div>
      <div className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
        {children}
      </div>
      {footer ? <div className="flex justify-end">{footer}</div> : null}
    </section>
  );
}

// LabeledControl wraps a control that does not label itself (e.g. SegmentedControl) with the same
// label + help-text treatment the ui.tsx fields use, so settings forms stay visually consistent.
export function LabeledControl({
  label,
  help,
  children,
}: {
  label: string;
  help?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div>
      <span className="block text-xs font-medium text-slate-600 dark:text-slate-300">{label}</span>
      <div className="mt-1">{children}</div>
      {help ? <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">{help}</p> : null}
    </div>
  );
}

// FieldHelp is the small help line rendered under a self-labelling ui.tsx field (TextField /
// SelectField) inside a settings form.
export function FieldHelp({ children }: { children: ReactNode }) {
  return <p className="mt-1 text-xs text-slate-500 dark:text-slate-400">{children}</p>;
}

// commonComponents.tsx reproduces the slice of Headlamp's `CommonComponents` module that plugins lay
// their content out with. A Headlamp plugin imports these from
// `@kinvolk/headlamp-plugin/lib/CommonComponents`, which the build maps to `pluginLib.CommonComponents`.
// KSail renders them with its own surface styling (Tailwind, not Material UI) so plugin content matches
// the host UI and needs no MUI dependency — a plugin using `<SectionBox title=…>` looks native in KSail.

import * as React from "react";

// SectionBox is Headlamp's titled content panel — the workhorse layout primitive plugins use to group
// detail content. The title is optional (some callers render a bare framed box).
export function SectionBox({
  title,
  children,
}: {
  title?: React.ReactNode;
  children?: React.ReactNode;
}): React.ReactElement {
  return (
    <section className="rounded-lg border border-slate-200 bg-white p-4 dark:border-slate-800 dark:bg-slate-900">
      {title === undefined ? null : (
        <h2 className="mb-3 text-sm font-semibold text-slate-700 dark:text-slate-200">{title}</h2>
      )}
      {children}
    </section>
  );
}

// SectionHeader renders a standalone section title, matching Headlamp's CommonComponents.SectionHeader
// for plugins that title content without wrapping it in a SectionBox.
export function SectionHeader({ title }: { title: React.ReactNode }): React.ReactElement {
  return <h2 className="mb-3 text-sm font-semibold text-slate-700 dark:text-slate-200">{title}</h2>;
}

// CommonComponents is the object assigned to pluginLib.CommonComponents (the module shape a plugin's
// CommonComponents import resolves to).
export const CommonComponents = { SectionBox, SectionHeader };

export type CommonComponentsShape = typeof CommonComponents;

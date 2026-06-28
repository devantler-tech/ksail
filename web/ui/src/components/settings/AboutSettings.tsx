import { ExternalLink } from "lucide-react";
import type { Config } from "../../api.ts";
import { surfaceLabel } from "../../lib/surface.ts";
import { Field } from "../Card.tsx";
import { SettingsSection } from "./SettingsSection.tsx";

const LINKS = [
  { label: "Documentation", href: "https://ksail.devantler.tech" },
  { label: "GitHub", href: "https://github.com/devantler-tech/ksail" },
];

// AboutSettings is the Settings page's "About" category: read-only deployment information and
// outbound links. The KSail version is shown once the backend reports it on /api/v1/config (added
// in a later phase); until then the row is simply omitted.
export function AboutSettings({ config }: { config: Config | null }) {
  const readOnly = config?.readOnly ?? false;
  const version = config?.version;

  return (
    <SettingsSection title="About" description="Details about this KSail web interface.">
      <dl className="divide-y divide-slate-100 dark:divide-slate-800">
        {version?.version ? (
          <Field label="Version">
            <span className="font-mono text-xs">{version.version}</span>
          </Field>
        ) : null}
        {version?.commit ? (
          <Field label="Commit">
            <span className="font-mono text-xs">{version.commit.slice(0, 12)}</span>
          </Field>
        ) : null}
        {version?.date ? <Field label="Built">{version.date}</Field> : null}
        <Field label="Surface">{surfaceLabel(config?.mode)}</Field>
        <Field label="Access">{readOnly ? "Read-only" : "Read-write"}</Field>
      </dl>

      <div className="mt-4 flex flex-wrap gap-2">
        {LINKS.map((link) => (
          <a
            key={link.href}
            href={link.href}
            target="_blank"
            rel="noreferrer noopener"
            className="inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium text-blue-600 ring-1 ring-inset ring-slate-200 transition-colors hover:bg-slate-50 dark:text-blue-400 dark:ring-slate-700 dark:hover:bg-slate-800"
          >
            {link.label}
            <ExternalLink className="size-3.5" aria-hidden />
          </a>
        ))}
      </div>
    </SettingsSection>
  );
}

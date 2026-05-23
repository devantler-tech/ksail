import { Check, CircleAlert, CircleCheck, CircleHelp, Copy, Pencil } from "lucide-react";
import { useState, type ReactNode } from "react";
import type { Cluster, Condition } from "../api.ts";
import { formatTimestamp, relativeAge } from "../lib/format.ts";
import { COMPONENT_LABELS, useMeta } from "../lib/meta.ts";
import { StatusBadge } from "./StatusBadge.tsx";
import { Button, SlideOver } from "./ui.tsx";

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid grid-cols-3 gap-3 py-2">
      <dt className="text-xs font-medium text-slate-500 dark:text-slate-400">{label}</dt>
      <dd className="col-span-2 text-sm text-slate-800 dark:text-slate-200">{children}</dd>
    </div>
  );
}

function CopyableEndpoint({ endpoint }: { endpoint: string }) {
  const [copied, setCopied] = useState(false);

  return (
    <button
      type="button"
      onClick={() => {
        // navigator.clipboard is undefined on non-secure origins / some browsers; guard so the
        // click never throws, and swallow a rejected write (e.g. denied permission).
        const clipboard = navigator.clipboard;
        if (!clipboard) {
          return;
        }
        clipboard
          .writeText(endpoint)
          .then(() => {
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1500);
          })
          .catch(() => {
            /* ignore: clipboard write unavailable or denied */
          });
      }}
      className="group inline-flex max-w-full items-center gap-1.5 rounded text-left"
      title="Copy endpoint"
    >
      <span className="truncate font-mono text-xs text-slate-700 dark:text-slate-300">{endpoint}</span>
      {copied ? (
        <Check className="size-3.5 shrink-0 text-emerald-500" aria-hidden />
      ) : (
        <Copy className="size-3.5 shrink-0 text-slate-400 group-hover:text-slate-600 dark:group-hover:text-slate-200" aria-hidden />
      )}
    </button>
  );
}

function conditionIcon(status: Condition["status"]) {
  if (status === "True") {
    return <CircleCheck className="size-4 text-emerald-500" aria-hidden />;
  }
  if (status === "False") {
    return <CircleAlert className="size-4 text-slate-400" aria-hidden />;
  }
  return <CircleHelp className="size-4 text-amber-500" aria-hidden />;
}

function Conditions({ conditions }: { conditions: Condition[] }) {
  if (conditions.length === 0) {
    return <p className="text-sm text-slate-400">No conditions reported.</p>;
  }

  return (
    <ul className="space-y-2">
      {conditions.map((condition) => (
        <li
          key={condition.type}
          className="rounded-lg border border-slate-200 p-3 dark:border-slate-800"
        >
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              {conditionIcon(condition.status)}
              <span className="text-sm font-medium text-slate-800 dark:text-slate-100">
                {condition.type}
              </span>
              {condition.reason ? (
                <span className="rounded bg-slate-100 px-1.5 py-0.5 text-xs text-slate-500 dark:bg-slate-800 dark:text-slate-400">
                  {condition.reason}
                </span>
              ) : null}
            </div>
            <span className="text-xs tabular-nums text-slate-400">
              {relativeAge(condition.lastTransitionTime)}
            </span>
          </div>
          {condition.message ? (
            <p className="mt-1.5 text-xs text-slate-500 dark:text-slate-400">{condition.message}</p>
          ) : null}
        </li>
      ))}
    </ul>
  );
}

function SectionTitle({ children }: { children: ReactNode }) {
  return (
    <h3 className="mb-1 mt-6 text-xs font-semibold uppercase tracking-wide text-slate-400 first:mt-0">
      {children}
    </h3>
  );
}

export function ClusterDetail({
  cluster,
  open,
  readOnly,
  onClose,
  onEdit,
}: {
  cluster: Cluster | null;
  open: boolean;
  readOnly: boolean;
  onClose: () => void;
  onEdit: (cluster: Cluster) => void;
}) {
  const meta = useMeta();
  const status = cluster?.status;
  const spec = cluster?.spec?.cluster;
  const namespace = cluster?.metadata.namespace ?? "default";
  const secret = status?.kubeconfigSecretRef;

  // Fall back to the API defaults (distribution zero-value, then the first provider the matrix lists
  // for it — which is what the operator resolves an unset provider to) for hand-written CRs.
  const distribution = spec?.distribution || meta.distributions[0] || "";
  const provider = spec?.provider || meta.providers[distribution]?.[0] || "";

  return (
    <SlideOver
      open={open && cluster !== null}
      onClose={onClose}
      title={cluster?.metadata.name ?? ""}
      subtitle={`namespace: ${namespace}`}
    >
      {cluster ? (
        <div>
          <div className="mb-2 flex items-center justify-between gap-2">
            <StatusBadge phase={status?.phase} />
            {!readOnly ? (
              <Button variant="secondary" size="sm" onClick={() => onEdit(cluster)}>
                <Pencil className="size-3.5" aria-hidden />
                Edit
              </Button>
            ) : null}
          </div>

          <SectionTitle>Spec</SectionTitle>
          <dl className="divide-y divide-slate-100 dark:divide-slate-800">
            <Row label="Distribution">{distribution}</Row>
            <Row label="Provider">{provider}</Row>
            <Row label="Control planes">{spec?.controlPlanes ?? 1}</Row>
            <Row label="Workers">{spec?.workers ?? 0}</Row>
            {meta.components.map((component) => (
              <Row key={component.key} label={COMPONENT_LABELS[component.key] ?? component.key}>
                {spec?.[component.key] || component.default}
              </Row>
            ))}
          </dl>

          <SectionTitle>Status</SectionTitle>
          <dl className="divide-y divide-slate-100 dark:divide-slate-800">
            <Row label="Phase">{status?.phase ?? "—"}</Row>
            <Row label="Endpoint">
              {status?.endpoint ? <CopyableEndpoint endpoint={status.endpoint} /> : "—"}
            </Row>
            <Row label="Nodes">
              {status?.nodesTotal === undefined
                ? "—"
                : `${status.nodesReady ?? 0} / ${status.nodesTotal} ready`}
            </Row>
            <Row label="Kubeconfig">
              {secret ? (
                <span className="font-mono text-xs">
                  {(secret.namespace ? `${secret.namespace}/` : "") + secret.name}
                </span>
              ) : (
                "—"
              )}
            </Row>
            <Row label="Created">
              <span title={formatTimestamp(cluster.metadata.creationTimestamp)}>
                {relativeAge(cluster.metadata.creationTimestamp)} ago
              </span>
            </Row>
            <Row label="Last reconcile">
              <span title={formatTimestamp(status?.lastReconcileTime)}>
                {status?.lastReconcileTime ? `${relativeAge(status.lastReconcileTime)} ago` : "—"}
              </span>
            </Row>
          </dl>

          <SectionTitle>Conditions</SectionTitle>
          <Conditions conditions={status?.conditions ?? []} />
        </div>
      ) : null}
    </SlideOver>
  );
}

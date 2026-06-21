import { Copy, ScrollText, SquareTerminal } from "lucide-react";
import { useMemo } from "react";
import {
  reconcileResource,
  restartResource,
  scaleResource,
  type K8sObject,
  type ResourceAction,
} from "../api.ts";
import { cx } from "../lib/cx.ts";
import { PluginDetailSections } from "../lib/plugins/PluginSlots.tsx";
import type { ResourceKindLists } from "../lib/meta.ts";
import { buildResourceTarget, objectConditions, toYaml } from "../lib/resources.ts";
import type { EventFields } from "../lib/k8s.ts";
import { EventList } from "./EventList.tsx";
import { StatusDot } from "./StatusBadge.tsx";
import { useToast } from "./Toast.tsx";
import { Button, SegmentedControl, SlideOver } from "./ui.tsx";

// ConditionsTable renders an object's status conditions; nothing when the object has none.
function ConditionsTable({ obj }: { obj: K8sObject }) {
  const conditions = objectConditions(obj);
  if (conditions.length === 0) {
    return null;
  }

  return (
    <section>
      <h4 className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400">Conditions</h4>
      <div className="overflow-hidden rounded-lg border border-slate-200 dark:border-slate-800">
        <table className="min-w-full divide-y divide-slate-200 text-sm dark:divide-slate-800">
          <tbody className="divide-y divide-slate-100 dark:divide-slate-800">
            {conditions.map((cond) => (
              <tr key={cond.type}>
                <td className="px-3 py-2 font-medium text-slate-700 dark:text-slate-200">{cond.type}</td>
                <td className="px-3 py-2">
                  <span
                    className={cx(
                      "inline-flex items-center gap-1.5",
                      cond.status === "True"
                        ? "text-emerald-600 dark:text-emerald-400"
                        : cond.status === "False"
                          ? "text-amber-600 dark:text-amber-400"
                          : "text-slate-500 dark:text-slate-400",
                    )}
                  >
                    <StatusDot tone={cond.status === "True" ? "ok" : cond.status === "False" ? "warn" : "muted"} />
                    {cond.status || "—"}
                  </span>
                </td>
                <td className="px-3 py-2 text-slate-600 dark:text-slate-300">
                  <div className="font-medium">{cond.reason || "—"}</div>
                  {cond.message ? <div className="text-xs text-slate-500 dark:text-slate-400">{cond.message}</div> : null}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}

// RelatedEvents renders the recent events that target the selected resource; nothing when there are
// none (or while loading produced none).
function RelatedEvents({ events }: { events: EventFields[] }) {
  if (events.length === 0) {
    return null;
  }

  return (
    <section>
      <h4 className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400">
        Related events
      </h4>
      <EventList events={events} />
    </section>
  );
}

// SimpleAction is one button-shaped write action (Restart/Reconcile): a button label, the toast verb,
// the kinds it applies to, and the API call it issues against the resource target. Scale is excluded
// (it carries a replicas input, so it is rendered as a form below).
interface SimpleAction {
  label: string;
  verb: string;
  kinds: readonly string[];
  call: (target: ResourceAction) => Promise<void>;
}

const SIMPLE_ACTIONS = (kindLists: ResourceKindLists): SimpleAction[] => [
  { label: "Restart", verb: "Restarted", kinds: kindLists.restartable, call: restartResource },
  { label: "Reconcile", verb: "Reconciling", kinds: kindLists.reconcilable, call: reconcileResource },
];

// ResourceDetailPanel is the slide-over for an inspected resource: a capability-gated action bar
// (scale/restart/reconcile, logs/exec for Pods, delete), its status conditions, the events targeting
// it, and its manifest in the chosen format. Write actions go through the parent's runAction (which
// owns the busy spinner, success toast, panel close, and list refresh); logs/exec and delete are
// handed back to the parent via the onOpen*/onRequestDelete callbacks.
export function ResourceDetailPanel({
  selected,
  kind,
  clusterId,
  kindLists,
  canWrite,
  canLogs,
  canExec,
  actionBusy,
  runAction,
  scaleValue,
  onScaleChange,
  detailFormat,
  onDetailFormatChange,
  relatedEvents,
  onClose,
  onOpenLogs,
  onOpenExec,
  onRequestDelete,
}: {
  selected: K8sObject | null;
  kind: string;
  clusterId: string;
  kindLists: ResourceKindLists;
  canWrite: boolean;
  canLogs: boolean;
  canExec: boolean;
  actionBusy: boolean;
  runAction: (verb: string, perform: () => Promise<void>) => void;
  scaleValue: string;
  onScaleChange: (value: string) => void;
  detailFormat: "yaml" | "json";
  onDetailFormatChange: (format: "yaml" | "json") => void;
  relatedEvents: EventFields[];
  onClose: () => void;
  onOpenLogs: (pod: K8sObject) => void;
  onOpenExec: (pod: K8sObject) => void;
  onRequestDelete: () => void;
}) {
  const toast = useToast();

  // The selected resource serialized for the manifest viewer, in the chosen format.
  const manifestText = useMemo(() => {
    if (!selected) {
      return "";
    }

    return detailFormat === "yaml" ? toYaml(selected) : JSON.stringify(selected, null, 2);
  }, [selected, detailFormat]);

  // copyManifest copies the serialized manifest to the clipboard and toasts the outcome.
  function copyManifest() {
    if (!navigator.clipboard) {
      toast.error("Clipboard unavailable");

      return;
    }

    navigator.clipboard
      .writeText(manifestText)
      .then(() => toast.success("Copied to clipboard"))
      .catch(() => toast.error("Copy failed"));
  }

  const isPod = kind === "Pod";
  const showActionBar = canWrite || ((canLogs || canExec) && isPod);

  return (
    <SlideOver
      open={selected !== null}
      onClose={onClose}
      title={selected?.metadata?.name ?? ""}
      subtitle={`${kind}${selected?.metadata?.namespace ? ` · ${selected.metadata.namespace}` : ""}`}
    >
      {selected ? (
        <div className="space-y-3">
          {showActionBar ? (
            <div className="flex flex-wrap items-center gap-2 border-b border-slate-200 pb-3 dark:border-slate-800">
              {canWrite && kindLists.scalable.includes(kind) ? (
                <form
                  className="flex items-center gap-1.5"
                  onSubmit={(event) => {
                    event.preventDefault();
                    const replicas = scaleValue.trim() === "" ? Number.NaN : Number(scaleValue);
                    if (!Number.isInteger(replicas) || replicas < 0) {
                      toast.error("Enter a non-negative whole number of replicas");

                      return;
                    }
                    runAction("Scaled", () => scaleResource(buildResourceTarget(clusterId, kind, selected), replicas));
                  }}
                >
                  <label className="flex items-center gap-1.5">
                    <span className="text-xs text-slate-500 dark:text-slate-400">Replicas</span>
                    <input
                      type="number"
                      min={0}
                      inputMode="numeric"
                      value={scaleValue}
                      onChange={(event) => onScaleChange(event.target.value)}
                      className="w-20 rounded-md border border-slate-300 bg-white px-2 py-1 text-sm dark:border-slate-700 dark:bg-slate-800 dark:text-slate-100"
                    />
                  </label>
                  <Button type="submit" size="sm" variant="secondary" loading={actionBusy}>
                    Scale
                  </Button>
                </form>
              ) : null}
              {canWrite
                ? SIMPLE_ACTIONS(kindLists)
                    .filter((action) => action.kinds.includes(kind))
                    .map((action) => (
                      <Button
                        key={action.label}
                        size="sm"
                        variant="secondary"
                        loading={actionBusy}
                        onClick={() => runAction(action.verb, () => action.call(buildResourceTarget(clusterId, kind, selected)))}
                      >
                        {action.label}
                      </Button>
                    ))
                : null}
              {canLogs && isPod ? (
                <Button size="sm" variant="secondary" onClick={() => onOpenLogs(selected)}>
                  <ScrollText className="size-3.5" aria-hidden />
                  Logs
                </Button>
              ) : null}
              {canExec && isPod ? (
                <Button size="sm" variant="secondary" onClick={() => onOpenExec(selected)}>
                  <SquareTerminal className="size-3.5" aria-hidden />
                  Exec
                </Button>
              ) : null}
              {canWrite && !kindLists.deleteDenied.includes(kind) ? (
                <Button size="sm" variant="danger" disabled={actionBusy} onClick={onRequestDelete}>
                  Delete
                </Button>
              ) : null}
            </div>
          ) : null}
          <ConditionsTable obj={selected} />
          <RelatedEvents events={relatedEvents} />
          {/* Plugin-contributed detail sections (Headlamp registerDetailsViewSection). Renders nothing
              until a plugin registers a section, so this is zero-cost by default. */}
          <PluginDetailSections resource={selected} />
          <section>
            <div className="mb-2 flex items-center justify-between">
              <SegmentedControl
                options={[
                  { value: "yaml", label: "YAML" },
                  { value: "json", label: "JSON" },
                ]}
                value={detailFormat}
                onChange={onDetailFormatChange}
              />
              <Button variant="ghost" size="sm" onClick={copyManifest}>
                <Copy className="size-3.5" aria-hidden />
                Copy
              </Button>
            </div>
            <pre className="overflow-x-auto rounded-lg bg-slate-50 p-3 text-xs leading-relaxed text-slate-800 dark:bg-slate-800/50 dark:text-slate-200">
              {manifestText}
            </pre>
          </section>
        </div>
      ) : null}
    </SlideOver>
  );
}

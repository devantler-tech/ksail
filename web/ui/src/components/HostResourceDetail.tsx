import { useEffect, useState } from "react";
import { errorMessage, type K8sObject } from "../api.ts";
import { getObjectByTarget, type ResourceTarget } from "../lib/plugins/k8s.ts";
import { ResourceDetailContent } from "./ResourceDetailPanel.tsx";
import { SlideOver } from "./ui.tsx";

// HostResourceDetail is the app-level resource-detail overlay opened when a Headlamp plugin links to a
// resource (a built-in kind or a custom resource). The plugin Link calls openResourceDetail (the
// resourceDetail bridge); App.tsx renders this for the active target. It fetches the object through the
// kube-proxy (getObjectByTarget) for the active cluster and renders the shared read-only
// ResourceDetailContent — the same body KSail's own resource browser uses — so a plugin "zoom into
// resource" link lands in KSail's native detail view instead of a dead link. It is read-only (no action
// bar): the object is reached by reference, outside the resource browser's write-action wiring.
//
// clusterNamespace/clusterName are the active cluster's coordinates passed as primitives (not an object)
// so the fetch effect's deps are stable across App re-renders — passing a fresh getCluster() object would
// refetch on every render.
export function HostResourceDetail({
  target,
  clusterNamespace,
  clusterName,
  onClose,
}: {
  target: ResourceTarget;
  clusterNamespace: string | null;
  clusterName: string | null;
  onClose: () => void;
}) {
  const [obj, setObj] = useState<K8sObject | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [detailFormat, setDetailFormat] = useState<"yaml" | "json">("yaml");

  useEffect(() => {
    if (!clusterName || !clusterNamespace) {
      setObj(null);
      setError("No active cluster");

      return undefined;
    }

    let cancelled = false;
    setObj(null);
    setError(null);

    getObjectByTarget({ namespace: clusterNamespace, name: clusterName }, target)
      .then((raw) => {
        if (!cancelled) {
          setObj(raw as K8sObject);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(errorMessage(err));
        }
      });

    return () => {
      cancelled = true;
    };
  }, [clusterNamespace, clusterName, target]);

  const subtitle = `${target.kind}${target.namespace ? ` · ${target.namespace}` : ""}`;

  return (
    <SlideOver open onClose={onClose} title={target.name} subtitle={subtitle}>
      {error ? (
        <div className="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-300">
          {error}
        </div>
      ) : obj ? (
        <ResourceDetailContent
          obj={obj}
          relatedEvents={[]}
          detailFormat={detailFormat}
          onDetailFormatChange={setDetailFormat}
        />
      ) : (
        <div className="p-4 text-sm text-slate-500 dark:text-slate-400">Loading…</div>
      )}
    </SlideOver>
  );
}
